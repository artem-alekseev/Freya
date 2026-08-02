package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SkillDamageInfo contains the four coefficients from skill_main/power atk.
// The client/server formula is:
//
//	(A*physicalAttack + B*magicAttack + C*skillLevel + D)/10
//
// Integer division intentionally truncates the complete expression, as in
// WorldSvr.
type SkillDamageInfo struct {
	PhysicalAttackCoef int
	MagicAttackCoef    int
	LevelAttackCoef    int
	BaseAttack         int
	Type               byte
	NormalSkillExp     uint16
	CriticalSkillExp   uint16
}

var skillDamageData = make(map[uint16]SkillDamageInfo)

func FindSkillDamage(skillID uint16) (SkillDamageInfo, bool) {
	info, ok := skillDamageData[skillID]
	return info, ok
}

func (info SkillDamageInfo) Experience() uint16 {
	return info.NormalSkillExp
}

// CriticalExperience returns the skill experience for a critical hit.
func (info SkillDamageInfo) CriticalExperience() uint16 {
	return info.CriticalSkillExp
}

// SkillSlotRange returns the client slot range reserved for a skill type.
// Physical skills occupy slots 0-31, magic skills occupy slots 32-63.
func SkillSlotRange(skillID uint16) (uint16, uint16, bool) {
	info, ok := skillDamageData[skillID]
	if !ok {
		return 0, 0, false
	}

	switch info.Type {
	case 1:
		return 0, 31, true
	case 2:
		return 32, 63, true
	default:
		return 0, 0, false
	}
}

func (info SkillDamageInfo) Calculate(physicalAttack, magicAttack int, level byte) int {
	if level == 0 {
		level = 1
	}

	levelIndex := int(level)
	// WorldSvr applies a small skill-level attack-amp delta before the
	// skill coefficients are evaluated. The delta is added only to the
	// matching physical or magical attack branch.
	deltaAmp := skillDeltaAttackAmp(levelIndex)
	physicalCoef := 10 * info.PhysicalAttackCoef
	magicCoef := 10 * info.MagicAttackCoef
	switch info.Type {
	case 1:
		physicalCoef += 5 * deltaAmp
	case 2:
		magicCoef += 5 * deltaAmp
	}

	damage := (physicalCoef*physicalAttack +
		magicCoef*magicAttack +
		10*info.LevelAttackCoef*levelIndex +
		10*info.BaseAttack) / 100
	if damage < 1 {
		damage = 1
	}
	return damage
}

// skillDeltaAttackAmp mirrors WorldSvr's FORMULA::SKILL::DeltaAMPBySkillLv.
func skillDeltaAttackAmp(level int) int {
	return level/10 + level/13 + level/16 + level/19
}

func parseSkillDamageData(cabalData []byte) (map[uint16]SkillDamageInfo, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	data := make(map[uint16]SkillDamageInfo)
	var skillID uint16
	var skillType byte
	var skillExp1 uint16
	var skillExp2 uint16
	var inSkillMain bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse skill damage data: %w", err)
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
				typeValue, err := uintAttribute(element, "type", 8)
				if err != nil {
					return nil, err
				}
				skillID = uint16(id)
				skillType = byte(typeValue)
				skillExp1 = 0
				skillExp2 = 0
				inSkillMain = true
			case "skill_param":
				if !inSkillMain {
					continue
				}
				exp1, err := uintAttribute(element, "exp1", 16)
				if err != nil {
					return nil, err
				}
				exp2, err := uintAttribute(element, "exp2", 16)
				if err != nil {
					return nil, err
				}
				skillExp1 = uint16(exp1)
				skillExp2 = uint16(exp2)
			case "power":
				if !inSkillMain {
					continue
				}
				attack, err := stringAttribute(element, "atk")
				if err != nil {
					return nil, err
				}
				values, err := parseSkillAttackCoefficients(attack)
				if err != nil {
					return nil, fmt.Errorf("skill %d: %w", skillID, err)
				}
				if _, exists := data[skillID]; exists {
					return nil, fmt.Errorf("duplicate skill damage data for skill %d", skillID)
				}
				values.Type = skillType
				values.NormalSkillExp = skillExp1
				values.CriticalSkillExp = skillExp2
				data[skillID] = values
			}
		case xml.EndElement:
			if element.Name.Local == "skill_main" {
				skillID = 0
				skillType = 0
				skillExp1 = 0
				skillExp2 = 0
				inSkillMain = false
			}
		}
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain skill damage data")
	}
	return data, nil
}

func parseSkillAttackCoefficients(value string) (SkillDamageInfo, error) {
	if strings.TrimSpace(value) == "" {
		// Passive skills may leave the attack field empty.
		return SkillDamageInfo{}, nil
	}

	parts := strings.Split(value, ",")
	if len(parts) == 2 {
		// Buff/passive skills use a two-value atk field and do not have
		// combat damage coefficients.
		for _, part := range parts {
			if _, err := strconv.Atoi(strings.TrimSpace(part)); err != nil {
				return SkillDamageInfo{}, fmt.Errorf("invalid skill attack coefficient %q: %w", part, err)
			}
		}
		return SkillDamageInfo{}, nil
	}
	if len(parts) != 4 {
		return SkillDamageInfo{}, fmt.Errorf("invalid skill attack coefficients %q", value)
	}

	coefficients := [4]int{}
	for i, part := range parts {
		coefficient, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return SkillDamageInfo{}, fmt.Errorf("invalid skill attack coefficient %q: %w", part, err)
		}
		coefficients[i] = coefficient
	}

	return SkillDamageInfo{
		PhysicalAttackCoef: coefficients[0],
		MagicAttackCoef:    coefficients[1],
		LevelAttackCoef:    coefficients[2],
		BaseAttack:         coefficients[3],
	}, nil
}

func stringAttribute(element xml.StartElement, name string) (string, error) {
	for _, attribute := range element.Attr {
		if attribute.Name.Local == name {
			return attribute.Value, nil
		}
	}
	return "", fmt.Errorf("element <%s> has no %s attribute", element.Name.Local, name)
}
