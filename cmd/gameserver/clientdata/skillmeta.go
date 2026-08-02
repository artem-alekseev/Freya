package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SkillMeta contains the parts of skill_main needed for server-side
// validation of self-targeted skills. In particular, group 32 is the Battle
// Mode group and exclusive is the required battle style.
type SkillMeta struct {
	Group               byte
	Exclusive           byte
	MPWastePerLevel     int
	MPWasteBase         int
	MPWasteModePerLevel int
	MPWasteModeBase     int
	SPWaste             int
	PreparationMS       int
}

var skillMetaData = make(map[uint16]SkillMeta)

func FindSkillMeta(skillID uint16) (SkillMeta, bool) {
	meta, ok := skillMetaData[skillID]
	return meta, ok
}

func (m SkillMeta) MPWaste(level byte) uint16 {
	if level == 0 {
		level = 1
	}
	// cabal.dec stores MP coefficients in tenths. WorldSvr calculates the
	// actual cost with integer division by 10.
	waste := (m.MPWastePerLevel*int(level) + m.MPWasteBase) / 10
	for _, threshold := range []int{10, 13, 16, 19} {
		waste += (int(level) / threshold) * m.MPWastePerLevel / 10
	}
	if waste <= 0 {
		return 0
	}
	if waste > 0xFFFF {
		return 0xFFFF
	}
	return uint16(waste)
}

// BattleModeMPWaste returns the additional percentage applied to a regular
// skill while this Battle Mode is active. This is the mpadd formula used by
// WorldSvr::Skill::BattleMode::AddBattleMode.
func (m SkillMeta) BattleModeMPWaste(masteryLevel, modeType byte) int {
	skillLevel := 0
	switch modeType {
	case 1:
		if masteryLevel < 4 {
			skillLevel = 1
		} else {
			skillLevel = int(masteryLevel) - 3
		}
	case 2:
		skillLevel = int(masteryLevel) - 5
		if skillLevel < 0 {
			skillLevel = 0
		}
	}
	return m.MPWasteModePerLevel*skillLevel + m.MPWasteModeBase
}

func parseSkillMetaData(cabalData []byte) (map[uint16]SkillMeta, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	data := make(map[uint16]SkillMeta)
	var (
		skillID      uint16
		meta         SkillMeta
		inSkillMain  bool
		conditionSet bool
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse skill metadata: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "skill_main":
				id, err := uintAttribute(element, "id", 16)
				if err != nil {
					return nil, err
				}
				if id == 0 {
					return nil, fmt.Errorf("skill_main has invalid id 0")
				}
				group, err := uintAttribute(element, "group", 8)
				if err != nil {
					return nil, err
				}
				if _, exists := data[uint16(id)]; exists {
					return nil, fmt.Errorf("duplicate skill metadata for skill %d", id)
				}
				skillID = uint16(id)
				meta = SkillMeta{Group: byte(group)}
				conditionSet = false
				inSkillMain = true

			case "condition":
				if !inSkillMain || conditionSet {
					continue
				}
				exclusive, hasExclusive, err := optionalUintAttribute(element, "exclusive", 8)
				if err != nil {
					return nil, err
				}
				if hasExclusive {
					meta.Exclusive = byte(exclusive)
					conditionSet = true
				}

			case "frame":
				if !inSkillMain {
					continue
				}
				term, hasTerm, err := optionalUintAttribute(element, "term", 32)
				if err != nil {
					return nil, err
				}
				if hasTerm {
					// The client stores the skill animation length in frames at
					// 30 FPS. WorldSvr uses the same value as the per-skill
					// preparation time before the first Battle Mode drain.
					meta.PreparationMS = int(term) * 1000 / 30
				}

			case "cost":
				if !inSkillMain {
					continue
				}
				mp, err := stringAttribute(element, "mp")
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(mp) == "" {
					continue
				}
				perLevel, base, err := parseSkillCost(mp)
				if err != nil {
					return nil, fmt.Errorf("skill %d: %w", skillID, err)
				}
				meta.MPWastePerLevel = perLevel
				meta.MPWasteBase = base

				spCost, err := stringAttribute(element, "sp")
				if err != nil {
					return nil, err
				}
				if strings.TrimSpace(spCost) != "" {
					meta.SPWaste, err = strconv.Atoi(strings.TrimSpace(spCost))
					if err != nil {
						return nil, fmt.Errorf("skill %d: invalid SP cost %q: %w", skillID, spCost, err)
					}
				}

				modeCost := ""
				for _, attr := range element.Attr {
					if attr.Name.Local == "mpadd" {
						modeCost = attr.Value
						break
					}
				}
				if strings.TrimSpace(modeCost) != "" {
					modePerLevel, modeBase, err := parseSkillCost(modeCost)
					if err != nil {
						return nil, fmt.Errorf("skill %d: invalid Battle Mode MP cost: %w", skillID, err)
					}
					meta.MPWasteModePerLevel = modePerLevel
					meta.MPWasteModeBase = modeBase
				}
			}

		case xml.EndElement:
			if element.Name.Local == "skill_main" && inSkillMain {
				data[skillID] = meta
				skillID = 0
				meta = SkillMeta{}
				conditionSet = false
				inSkillMain = false
			}
		}
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain skill metadata")
	}
	return data, nil
}

func parseSkillCost(value string) (int, int, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid MP cost %q", value)
	}
	perLevel, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid MP cost %q: %w", value, err)
	}
	base, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid MP cost %q: %w", value, err)
	}
	return perLevel, base, nil
}
