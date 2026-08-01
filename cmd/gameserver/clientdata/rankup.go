package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

const maxSkillRank byte = 10

// SkillRankProgress contains the client values needed to turn skill
// experience into a skill point and to promote a sword or magic rank.
type SkillRankProgress struct {
	SwordExpForPoint uint16
	MagicExpForPoint uint16
	SwordRankPoints  uint16
	MagicRankPoints  uint16
}

// SkillRankBonus contains the permanent stat bonus granted on promotion from
// the specified rank.
type SkillRankBonus struct {
	STR uint32
	DEX uint32
	INT uint32
}

type skillRankKey struct {
	style byte
	rank  byte
}

type rankConditionValues struct {
	sword [6]uint16
	magic [6]uint16
}

var skillRankProgressData = make(map[skillRankKey]SkillRankProgress)
var skillRankBonusData = make(map[[2]byte]SkillRankBonus)

var skillRankClassNames = map[byte]string{
	1: "twohand",
	2: "dual",
	3: "magic",
	4: "magic_arrow",
	5: "sword_shield",
	6: "magic_sword",
}

// FindSkillRankProgress returns the thresholds for the character style and
// current skill rank. skillType is 1 for sword and 2 for magic.
func FindSkillRankProgress(style, rank, skillType byte) (SkillRankProgress, bool) {
	if style < 1 || style > 6 || rank < 1 || rank > maxSkillRank ||
		(skillType != 1 && skillType != 2) {
		return SkillRankProgress{}, false
	}

	progress, ok := skillRankProgressData[skillRankKey{style: style, rank: rank}]
	return progress, ok
}

// FindSkillRankBonus returns the WorldSvr bonus applied when the specified
// sword or magic rank is promoted to the next rank.
func FindSkillRankBonus(rank, skillType byte) (SkillRankBonus, bool) {
	if rank < 1 || rank >= maxSkillRank || (skillType != 1 && skillType != 2) {
		return SkillRankBonus{}, false
	}
	bonus, ok := skillRankBonusData[[2]byte{rank, skillType}]
	return bonus, ok
}

func parseSkillRankData(cabalData []byte) (map[skillRankKey]SkillRankProgress, map[[2]byte]SkillRankBonus, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	conditions := make(map[uint16]rankConditionValues)
	progress := make(map[skillRankKey]SkillRankProgress)
	bonuses := make(map[[2]byte]SkillRankBonus)

	var (
		inRankupCondition bool
		conditionRank     uint16
		inExpForPoint     bool
		expForPointRank   uint16
		inRankBonus       bool
		bonusRank         uint16
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("parse skill rank data: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "rankup_condition":
				rank, err := uintAttribute(element, "rank", 16)
				if err != nil {
					return nil, nil, err
				}
				conditionRank = uint16(rank)
				inRankupCondition = true
			case "exp_for_point":
				rank, err := uintAttribute(element, "rank", 16)
				if err != nil {
					return nil, nil, err
				}
				expForPointRank = uint16(rank)
				inExpForPoint = true
			case "rank_bonus":
				rank, err := uintAttribute(element, "readyrank", 16)
				if err != nil || rank >= uint64(maxSkillRank) {
					if err == nil {
						err = fmt.Errorf("invalid rank bonus rank %d", rank)
					}
					return nil, nil, err
				}
				bonusRank = uint16(rank) + 1
				inRankBonus = true
			case "condition":
				if inRankupCondition {
					conditionType, err := uintAttribute(element, "type", 8)
					if err != nil || (conditionType != 0 && conditionType != 1) {
						if err == nil {
							err = fmt.Errorf("invalid rankup condition type %d", conditionType)
						}
						return nil, nil, err
					}
					values := conditions[conditionRank]
					for style, className := range skillRankClassNames {
						value, err := uintAttribute(element, className, 16)
						if err != nil {
							return nil, nil, err
						}
						if conditionType == 0 {
							values.sword[style-1] = uint16(value)
						} else {
							values.magic[style-1] = uint16(value)
						}
					}
					conditions[conditionRank] = values
				} else if inExpForPoint {
					className, err := stringAttribute(element, "class")
					if err != nil {
						return nil, nil, err
					}
					style := styleForSkillClass(className)
					if style == 0 {
						return nil, nil, fmt.Errorf("unknown skill rank class %q", className)
					}
					sword, err := uintAttribute(element, "sword", 16)
					if err != nil {
						return nil, nil, err
					}
					magic, err := uintAttribute(element, "magic", 16)
					if err != nil {
						return nil, nil, err
					}
					key := skillRankKey{style: style, rank: byte(expForPointRank)}
					current := progress[key]
					current.SwordExpForPoint = uint16(sword)
					current.MagicExpForPoint = uint16(magic)
					progress[key] = current
				} else if inRankBonus {
					bonusType, err := uintAttribute(element, "type", 8)
					if err != nil || (bonusType != 0 && bonusType != 1) {
						if err == nil {
							err = fmt.Errorf("invalid rank bonus type %d", bonusType)
						}
						return nil, nil, err
					}
					bonus := SkillRankBonus{
						STR: uint32(optionalIntAttribute(element, "str")),
						DEX: uint32(optionalIntAttribute(element, "dex")),
						INT: uint32(optionalIntAttribute(element, "int")),
					}
					bonuses[[2]byte{byte(bonusRank), byte(bonusType + 1)}] = bonus
				}
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "rankup_condition":
				inRankupCondition = false
			case "exp_for_point":
				inExpForPoint = false
			case "rank_bonus":
				inRankBonus = false
			}
		}
	}

	for rank := byte(1); rank <= maxSkillRank; rank++ {
		values, ok := conditions[uint16(rank)]
		if !ok {
			return nil, nil, fmt.Errorf("cabal.dec has no rankup condition for rank %d", rank)
		}
		for style := byte(1); style <= 6; style++ {
			key := skillRankKey{style: style, rank: rank}
			current, ok := progress[key]
			if !ok || current.SwordExpForPoint == 0 || current.MagicExpForPoint == 0 {
				return nil, nil, fmt.Errorf("cabal.dec has no exp_for_point for style %d rank %d", style, rank)
			}
			current.SwordRankPoints = values.sword[style-1]
			current.MagicRankPoints = values.magic[style-1]
			if current.SwordRankPoints == 0 || current.MagicRankPoints == 0 {
				return nil, nil, fmt.Errorf("cabal.dec has no rankup condition for style %d rank %d", style, rank)
			}
			progress[key] = current
		}
	}

	return progress, bonuses, nil
}

func styleForSkillClass(className string) byte {
	if className == "2hand" {
		return 1
	}

	for style, name := range skillRankClassNames {
		if name == className {
			return style
		}
	}
	return 0
}

func optionalIntAttribute(element xml.StartElement, name string) int64 {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != name || attribute.Value == "" {
			continue
		}
		value, err := strconv.ParseInt(attribute.Value, 10, 64)
		if err == nil {
			return value
		}
	}
	return 0
}
