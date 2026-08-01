package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

// CharacterLevel describes an entry from cabal.dec's level_up table.
// RequiredExp is the experience needed to advance from Level to Level + 1.
// AccumulatedExp is the total experience threshold for that advancement.
type CharacterLevel struct {
	Level          uint16
	RequiredExp    uint64
	AccumulatedExp uint64
}

var characterLevels = make(map[uint16]CharacterLevel)

func FindCharacterLevel(level uint16) (CharacterLevel, bool) {
	value, ok := characterLevels[level]
	return value, ok
}

// LevelForExperience returns the level matching total experience, using the
// thresholds from the target client's cabal.dec file.
func LevelForExperience(currentLevel uint16, experience uint64) uint16 {
	if currentLevel == 0 {
		currentLevel = 1
	}

	for {
		entry, ok := characterLevels[currentLevel]
		if !ok || experience < entry.AccumulatedExp {
			return currentLevel
		}
		nextLevel := currentLevel + 1
		if _, ok := characterLevels[nextLevel]; !ok {
			return currentLevel
		}
		currentLevel = nextLevel
	}
}

func parseCharacterLevels(cabalData []byte) (map[uint16]CharacterLevel, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	loaded := make(map[uint16]CharacterLevel)
	inLevelUp := false

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse cabal.dec XML: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "level_up":
				inLevelUp = true
			case "con":
				if !inLevelUp {
					continue
				}
				level, err := uintAttribute(element, "level", 16)
				if err != nil {
					return nil, err
				}
				required, err := uintAttribute(element, "exp", 64)
				if err != nil {
					return nil, err
				}
				accumulated, err := uintAttribute(element, "accuexp", 64)
				if err != nil {
					return nil, err
				}
				if level == 0 || accumulated < required {
					return nil, fmt.Errorf("invalid level_up entry for level %d", level)
				}
				key := uint16(level)
				if _, exists := loaded[key]; exists {
					return nil, fmt.Errorf("duplicate level_up entry for level %d", level)
				}
				loaded[key] = CharacterLevel{
					Level:          key,
					RequiredExp:    required,
					AccumulatedExp: accumulated,
				}
			}
		case xml.EndElement:
			if element.Name.Local == "level_up" {
				inLevelUp = false
			}
		}
	}

	if len(loaded) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a level_up table")
	}

	return loaded, nil
}
