package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// BattleModeSkill describes a skill granted by a battle-style mastery level.
type BattleModeSkill struct {
	SkillID uint16
	Slot    uint16
}

const (
	firstBattleModeRank           byte   = 1 // mastery level 1 contains Combo (skill 419)
	masterySkillSlotOffset        uint64 = 34
	battleModeAdditionalSkillSlot uint16 = 76
	battleModeBasicSkillSlot      uint16 = 78
)

var battleModeSkillData = make(map[byte]map[byte][]BattleModeSkill)

var masterySkillClassNames = map[string]byte{
	"2hand":        1,
	"dual":         2,
	"magic":        3,
	"magic_arrow":  4,
	"sword_shield": 5,
	"magic_sword":  6,
}

// Battle-mode basic attacks occupy the client-reserved slots 78-81. The
// mapping comes from the skill_order entries in enc/cabal.dec. Not every
// style has four entries in this client: the magic style has no matching
// entries, and the sword-and-shield style has only three.
var battleModeBasicSkillsByStyle = map[byte][]uint16{
	1: {380, 381, 382, 383}, // 2hand
	2: {384, 385, 386, 387}, // dual
	3: nil,                  // magic: no 78-81 skills in this client data
	4: {391, 392, 393, 394}, // magic_arrow
	5: {388, 389, 390},      // sword_shield
	6: {464, 465, 466, 467}, // magic_sword
}

// BattleModeMasteryLevelForCharacterLevel converts the character level to the
// battle-style mastery level used by mastery_levelup in cabal.dec. Level 40
// has no new battle-mode skill; it only raises the character rank. The
// sixth mastery rank contains the optional BM2 follow-up skills, which are
// intentionally not granted by the server.
func BattleModeMasteryLevelForCharacterLevel(level uint16) byte {
	switch {
	case level >= 60:
		return 5
	case level >= 50:
		return 5
	case level >= 30:
		return 3
	case level >= 20:
		return 2
	case level >= 10:
		return 1
	default:
		return 0
	}
}

// FindBattleModeSkills returns all battle-mode skills that should be learned
// by the given battle style at the specified mastery level.
func FindBattleModeSkills(style, masteryLevel byte) []BattleModeSkill {
	if style < 1 || style > 6 || masteryLevel < firstBattleModeRank {
		return nil
	}

	byRank, ok := battleModeSkillData[style]
	if !ok {
		return nil
	}

	result := make([]BattleModeSkill, 0)
	seen := make(map[uint16]struct{})
	for rank := firstBattleModeRank; rank <= masteryLevel; rank++ {
		for _, skill := range byRank[rank] {
			if _, exists := seen[skill.SkillID]; exists {
				continue
			}
			seen[skill.SkillID] = struct{}{}
			result = append(result, skill)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Slot < result[j].Slot
	})
	return result
}

// FindBattleModeSkillsForLevel returns the battle-mode skills available to a
// character at the given character level.
func FindBattleModeSkillsForLevel(style byte, level uint16) []BattleModeSkill {
	result := FindBattleModeSkills(style, BattleModeMasteryLevelForCharacterLevel(level))
	for index, skillID := range battleModeBasicSkillsByStyle[style] {
		result = append(result, BattleModeSkill{
			SkillID: skillID,
			Slot:    battleModeBasicSkillSlot + uint16(index),
		})
	}

	if len(result) > 1 {
		sort.Slice(result, func(i, j int) bool {
			return result[i].Slot < result[j].Slot
		})
	}
	return result
}

// BattleModeTypeForSkill identifies whether a mastery skill is the BM1 or
// BM2 activation skill. The group check is deliberately kept outside this
// function because mastery level 5 also contains a regular follow-up skill.
func BattleModeTypeForSkill(style byte, skillID uint16) (byte, bool) {
	byRank, ok := battleModeSkillData[style]
	if !ok {
		return 0, false
	}

	for rank, skills := range byRank {
		modeType := byte(0)
		switch rank {
		case 3:
			modeType = 1
		case 5:
			modeType = 2
		default:
			continue
		}
		for _, skill := range skills {
			if skill.SkillID == skillID {
				return modeType, true
			}
		}
	}
	return 0, false
}

func parseBattleModeSkills(cabalData []byte) (map[byte]map[byte][]BattleModeSkill, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	data := make(map[byte]map[byte][]BattleModeSkill)
	var (
		inMasteryLevelup bool
		masteryLevel     byte
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse battle mode skill data: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "mastery_levelup":
				level, err := uintAttribute(element, "level", 8)
				if err != nil {
					return nil, err
				}
				masteryLevel = byte(level)
				inMasteryLevelup = true
			case "condition":
				if !inMasteryLevelup {
					continue
				}
				styleName, err := stringAttribute(element, "class")
				if err != nil {
					return nil, err
				}
				style := masterySkillClassNames[styleName]
				if style == 0 {
					return nil, fmt.Errorf("unknown mastery skill class %q", styleName)
				}

				for index := 1; index <= 2; index++ {
					skillID, hasSkill, err := optionalUintAttribute(element, fmt.Sprintf("skill%didx", index), 16)
					if err != nil {
						return nil, err
					}
					slot, hasSlot, err := optionalUintAttribute(element, fmt.Sprintf("skill%dslotidx", index), 16)
					if err != nil {
						return nil, err
					}
					if !hasSkill && !hasSlot {
						continue
					}
					if !hasSkill || !hasSlot || skillID == 0 {
						return nil, fmt.Errorf("incomplete mastery skill %d for class %q", index, styleName)
					}
					targetSlot := uint16(slot + masterySkillSlotOffset)
					if masteryLevel == 5 && index == 2 {
						targetSlot = battleModeAdditionalSkillSlot
					}

					if data[style] == nil {
						data[style] = make(map[byte][]BattleModeSkill)
					}
					data[style][masteryLevel] = append(data[style][masteryLevel], BattleModeSkill{
						SkillID: uint16(skillID),
						Slot:    targetSlot,
					})
				}
			}
		case xml.EndElement:
			if element.Name.Local == "mastery_levelup" {
				inMasteryLevelup = false
			}
		}
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain mastery_levelup skill data")
	}
	return data, nil
}

func optionalUintAttribute(element xml.StartElement, name string, bitSize int) (uint64, bool, error) {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != name {
			continue
		}
		if attribute.Value == "" {
			return 0, false, nil
		}

		value, err := strconv.ParseUint(attribute.Value, 10, bitSize)
		if err != nil {
			return 0, false, fmt.Errorf("invalid %s attribute %q on <%s>: %w",
				name, attribute.Value, element.Name.Local, err)
		}
		return value, true, nil
	}

	return 0, false, nil
}
