package clientdata

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

const (
	itemKindIndexMask            uint32 = 0x00001FFF
	upgradeCoreMask              uint32 = 0x0001E000
	upgradeCoreShift                    = 13
	itemTypeOffsetInRecord              = 0
	physicalAttackOffsetInRecord        = 343
	itemGradeOffsetInRecord             = 387
	// These are the local EP6 item.dec values. They differ from the EP8
	// template's enum values: type 5 is a one-handed blade, while 4 and 6
	// are the crystal/orb magic weapons.
	itemTypeTwoHandWeapon = 1
	itemTypeMagicCrystal  = 4
	itemTypeOneHandWeapon = 5
	itemTypeMagicOrb      = 6
)

type weaponClass byte

const (
	weaponClassMagic weaponClass = iota + 1
	weaponClassOneHand
	weaponClassTwoHand
)

type weaponAttackInfo struct {
	baseAttack   int
	upgradeBonus [16]int
}

type weaponUpgradeFormula struct {
	minGrade int
	maxGrade int
	valueA   int
	valueB   int
}

// Filled by Initialize from every weapon record in item.dec.
var weaponAttackData = make(map[uint32]weaponAttackInfo)

type clientEnchantDocument struct {
	Enchant clientEnchantSection `xml:"enchant"`
}

type clientEnchantSection struct {
	Items []clientEnchantItem `xml:"item"`
}

type clientEnchantItem struct {
	Type   string               `xml:"type,attr"`
	Grades []clientEnchantGrade `xml:"grade"`
}

type clientEnchantGrade struct {
	MinGrade int                  `xml:"min_grade,attr"`
	MaxGrade int                  `xml:"max_grade,attr"`
	Values   []clientEnchantValue `xml:"enchant_value"`
}

type clientEnchantValue struct {
	ForceID    int    `xml:"force_id,attr"`
	ForceValue string `xml:"force_value,attr"`
}

func parseWeaponUpgradeFormulas(data []byte) (map[weaponClass][]weaponUpgradeFormula, error) {
	xmlStart := bytes.Index(data, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain an XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(data[xmlStart:]))
	var document clientEnchantDocument
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse client enchant data: %w", err)
	}

	itemTypes := map[string]weaponClass{
		"MG": weaponClassMagic,
		"1H": weaponClassOneHand,
		"2H": weaponClassTwoHand,
	}
	attackForceIDs := map[weaponClass]int{
		weaponClassMagic:   3,
		weaponClassOneHand: 3,
		weaponClassTwoHand: 23,
	}
	formulas := make(map[weaponClass][]weaponUpgradeFormula)

	for _, item := range document.Enchant.Items {
		itemType, ok := itemTypes[item.Type]
		if !ok {
			continue
		}

		for _, grade := range item.Grades {
			for _, value := range grade.Values {
				if value.ForceID != attackForceIDs[itemType] {
					continue
				}

				parts := strings.Split(value.ForceValue, ",")
				if len(parts) != 2 {
					return nil, fmt.Errorf("invalid client enchant value %q", value.ForceValue)
				}
				valueA, err := strconv.Atoi(strings.TrimSpace(parts[0]))
				if err != nil {
					return nil, fmt.Errorf("invalid client enchant value %q: %w", value.ForceValue, err)
				}
				valueB, err := strconv.Atoi(strings.TrimSpace(parts[1]))
				if err != nil {
					return nil, fmt.Errorf("invalid client enchant value %q: %w", value.ForceValue, err)
				}

				formulas[itemType] = append(formulas[itemType], weaponUpgradeFormula{
					minGrade: grade.MinGrade,
					maxGrade: grade.MaxGrade,
					valueA:   valueA,
					valueB:   valueB,
				})
			}
		}
	}

	for _, class := range []weaponClass{weaponClassMagic, weaponClassOneHand, weaponClassTwoHand} {
		if len(formulas[class]) == 0 {
			return nil, fmt.Errorf("client enchant data has no attack formula for weapon class %d", class)
		}
	}

	return formulas, nil
}

func parseWeaponAttackData(data []byte, layout itemFileLayout, formulas map[weaponClass][]weaponUpgradeFormula) map[uint32]weaponAttackInfo {
	weapons := make(map[uint32]weaponAttackInfo)
	if layout.recordSize < physicalAttackOffsetInRecord+4 {
		return weapons
	}

	for itemID, recordStart := uint32(1), layout.headerSize; recordStart+layout.recordSize <= len(data); itemID, recordStart = itemID+1, recordStart+layout.recordSize {
		itemType := binary.LittleEndian.Uint32(data[recordStart+itemTypeOffsetInRecord : recordStart+itemTypeOffsetInRecord+4])
		itemClass, ok := localWeaponClass(itemType)
		if !ok {
			continue
		}

		attack := binary.LittleEndian.Uint32(data[recordStart+physicalAttackOffsetInRecord : recordStart+physicalAttackOffsetInRecord+4])
		if attack == 0 {
			continue
		}

		grade := 1
		if layout.recordSize >= itemGradeOffsetInRecord+4 {
			grade = int(binary.LittleEndian.Uint32(data[recordStart+itemGradeOffsetInRecord : recordStart+itemGradeOffsetInRecord+4]))
			if grade == 0 {
				grade = 1
			}
		}

		formula := formulas[itemClass][0]
		for _, candidate := range formulas[itemClass] {
			if grade >= candidate.minGrade && grade <= candidate.maxGrade {
				formula = candidate
				break
			}
		}

		var upgradeBonus [16]int
		for level := 1; level < len(upgradeBonus); level++ {
			upgradeBonus[level] = upgradeBonus[level-1] + formula.valueA + ((level-1)/3)*formula.valueB
		}
		weapons[itemID] = weaponAttackInfo{baseAttack: int(attack), upgradeBonus: upgradeBonus}
	}

	return weapons
}

func localWeaponClass(itemType uint32) (weaponClass, bool) {
	switch itemType {
	case itemTypeTwoHandWeapon:
		return weaponClassTwoHand, true
	case itemTypeMagicCrystal, itemTypeMagicOrb:
		return weaponClassMagic, true
	case itemTypeOneHandWeapon:
		return weaponClassOneHand, true
	default:
		return 0, false
	}
}

// FindWeaponAttack returns the physical attack supplied by an equipped
// weapon, including its upgrade level.
func FindWeaponAttack(kind uint32) (int, bool) {
	if kind == 0 {
		return 0, false
	}

	itemIndex := kind & itemKindIndexMask
	data, ok := weaponAttackData[itemIndex]
	if !ok {
		return 0, false
	}

	upgradeLevel := int((kind & upgradeCoreMask) >> upgradeCoreShift)
	return data.baseAttack + data.upgradeBonus[upgradeLevel], true
}
