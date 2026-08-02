package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SkillBuffEffect is one force effect from cabal.dec/skill_value.
// WorldSvr calculates its value as (valueA*skillLevel + valueB) / 10.
type SkillBuffEffect struct {
	ForceID   int
	ValueA    int
	ValueB    int
	DurationA int
	DurationB int
	ValueType byte
}

func (e SkillBuffEffect) Value(level byte) int {
	if level == 0 {
		level = 1
	}
	return (e.ValueA*int(level) + e.ValueB) / 10
}

func (e SkillBuffEffect) Duration(level byte) int {
	if level == 0 {
		level = 1
	}
	return e.DurationA*int(level) + e.DurationB
}

// SkillBuffInfo contains the effects that the server applies to a target.
type SkillBuffInfo struct {
	Effects []SkillBuffEffect
}

var skillBuffData = make(map[uint16]SkillBuffInfo)

func FindSkillBuff(skillID uint16) (SkillBuffInfo, bool) {
	info, ok := skillBuffData[skillID]
	return info, ok
}

func (info SkillBuffInfo) Duration(level byte) int {
	result := 0
	for _, effect := range info.Effects {
		duration := effect.Duration(level)
		if duration <= 0 {
			continue
		}
		if result == 0 || duration < result {
			result = duration
		}
	}
	return result
}

func parseSkillBuffData(cabalData []byte) (map[uint16]SkillBuffInfo, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	data := make(map[uint16]SkillBuffInfo)
	var skillID uint16
	var info SkillBuffInfo
	var inSkillValue bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse skill buff data: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "skill_value":
				id, err := uintAttribute(element, "id", 16)
				if err != nil {
					return nil, err
				}
				if id == 0 {
					return nil, fmt.Errorf("skill_value has invalid id 0")
				}
				if _, exists := data[uint16(id)]; exists {
					return nil, fmt.Errorf("duplicate skill buff data for skill %d", id)
				}
				skillID = uint16(id)
				info = SkillBuffInfo{}
				inSkillValue = true

			case "value":
				if !inSkillValue {
					continue
				}
				forceID, err := uintAttribute(element, "bforce_id", 16)
				if err != nil {
					return nil, err
				}
				forceValue, err := stringAttribute(element, "bforce_value")
				if err != nil {
					return nil, err
				}
				valueA, valueB, err := parseSkillBuffPair(forceValue)
				if err != nil {
					return nil, fmt.Errorf("skill %d: invalid buff value %q: %w", skillID, forceValue, err)
				}

				duration := ""
				for _, attribute := range element.Attr {
					if attribute.Name.Local == "dur" {
						duration = attribute.Value
						break
					}
				}
				durationA, durationB, err := parseSkillBuffPair(duration)
				if err != nil {
					return nil, fmt.Errorf("skill %d: invalid buff duration %q: %w", skillID, duration, err)
				}

				valueType, hasValueType, err := optionalUintAttribute(element, "value_type", 8)
				if err != nil {
					return nil, err
				}
				if !hasValueType {
					valueType = 1
				}

				info.Effects = append(info.Effects, SkillBuffEffect{
					ForceID:   int(forceID),
					ValueA:    valueA,
					ValueB:    valueB,
					DurationA: durationA,
					DurationB: durationB,
					ValueType: byte(valueType),
				})
			}

		case xml.EndElement:
			if element.Name.Local == "skill_value" && inSkillValue {
				data[skillID] = info
				skillID = 0
				info = SkillBuffInfo{}
				inSkillValue = false
			}
		}
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain skill buff data")
	}
	return data, nil
}

func parseSkillBuffPair(value string) (int, int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, 0, nil
	}

	parts := strings.Split(value, ",")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("expected two coefficients")
	}
	first, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	second, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	return first, second, nil
}
