package clientdata

import (
	"bytes"
	cryptorand "crypto/rand"
	"encoding/xml"
	"fmt"
	"math/big"
)

const (
	forceCoreRateScale     uint64 = 1_000_000_000
	forceCorePerStone      uint64 = 50_000_000
	forceCoreOptionSlots          = 4
	forceCoreOptionMask    uint32 = 0x0F
	forceCoreOptionNumMask uint32 = 0x70
	forceCoreEpicMask      uint32 = 0x80
)

type forceCoreRateKey struct {
	activeOptions int
	level         int
}

type forceCoreOption struct {
	Index     int
	ForceCode int
}

type forceCoreDataDocument struct {
	ForceCore forceCoreDataSection `xml:"force_core"`
}

type forceCoreDataSection struct {
	Codes      []forceCoreCodeGroup `xml:"code"`
	RateInit   forceCoreRateInit    `xml:"fcore_rate_init"`
	ChangeInit forceCoreChangeInit  `xml:"fcore_change_init"`
}

type forceCoreCodeGroup struct {
	Type    string               `xml:"type,attr"`
	Options []forceCoreCodeEntry `xml:"cont"`
}

type forceCoreCodeEntry struct {
	Index     int `xml:"index,attr"`
	ForceCode int `xml:"code,attr"`
}

type forceCoreRateInit struct {
	Rates []forceCoreRateEntry `xml:"fcore_rate"`
}

type forceCoreRateEntry struct {
	ActiveOptions int    `xml:"active_opt,attr"`
	Level         int    `xml:"enchant_level,attr"`
	Rate          uint64 `xml:"rate,attr"`
}

type forceCoreChangeInit struct {
	Changes []forceCoreChangeEntry `xml:"fcore_change"`
}

type forceCoreChangeEntry struct {
	OneHandCode int `xml:"onehand_code,attr"`
	TwoHandCode int `xml:"twohand_code,attr"`
}

var (
	forceCoreRates   = make(map[forceCoreRateKey]uint64)
	forceCoreOptions = make(map[string][]forceCoreOption)
	forceCoreChanges = make(map[int]int)
)

func parseForceCoreData(cabalData []byte) (map[forceCoreRateKey]uint64, map[string][]forceCoreOption, map[int]int, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, nil, nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	var document forceCoreDataDocument
	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	if err := decoder.Decode(&document); err != nil {
		return nil, nil, nil, fmt.Errorf("parse cabal.dec force-core data: %w", err)
	}

	rates := make(map[forceCoreRateKey]uint64, len(document.ForceCore.RateInit.Rates))
	for _, entry := range document.ForceCore.RateInit.Rates {
		if entry.ActiveOptions < 0 || entry.ActiveOptions > 4 || entry.Level < 0 || entry.Level > 15 || entry.Rate > forceCoreRateScale {
			return nil, nil, nil, fmt.Errorf("invalid fcore_rate active_opt=%d enchant_level=%d rate=%d", entry.ActiveOptions, entry.Level, entry.Rate)
		}
		rates[forceCoreRateKey{activeOptions: entry.ActiveOptions, level: entry.Level}] = entry.Rate
	}
	if len(rates) == 0 {
		return nil, nil, nil, fmt.Errorf("cabal.dec does not contain fcore_rate data")
	}

	options := make(map[string][]forceCoreOption, len(document.ForceCore.Codes))
	for _, group := range document.ForceCore.Codes {
		for _, entry := range group.Options {
			if entry.Index <= 0 || entry.Index > 15 || entry.ForceCode <= 0 {
				continue
			}
			options[group.Type] = append(options[group.Type], forceCoreOption{
				Index:     entry.Index,
				ForceCode: entry.ForceCode,
			})
		}
	}

	changes := make(map[int]int, len(document.ForceCore.ChangeInit.Changes))
	for _, entry := range document.ForceCore.ChangeInit.Changes {
		if entry.OneHandCode > 0 && entry.TwoHandCode > 0 {
			changes[entry.OneHandCode] = entry.TwoHandCode
		}
	}

	return rates, options, changes, nil
}

// ForceCoreSuccessRate returns the client rate plus the server's 5% bonus for
// every submitted force core. Rates use the same 0..1,000,000,000 scale as
// cabal.dec.
func ForceCoreSuccessRate(kind uint32, itemOption int32, forceCoreNum byte) (uint64, bool) {
	level := int((kind & upgradeCoreMask) >> upgradeCoreShift)
	activeOptions := forceCoreActiveOptions(itemOption)
	baseRate, ok := forceCoreRates[forceCoreRateKey{activeOptions: activeOptions, level: level}]
	if !ok {
		return 0, false
	}

	rate := baseRate + uint64(forceCoreNum)*forceCorePerStone
	if rate > forceCoreRateScale {
		rate = forceCoreRateScale
	}
	return rate, true
}

// RollForceCoreSuccess performs one server-side roll against the exact rate
// scale used by the client data.
func RollForceCoreSuccess(kind uint32, itemOption int32, forceCoreNum byte) (bool, uint64, bool) {
	rate, ok := ForceCoreSuccessRate(kind, itemOption, forceCoreNum)
	if !ok {
		return false, 0, false
	}

	roll, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(forceCoreRateScale)))
	if err != nil {
		return false, rate, false
	}
	return uint64(roll.Int64()) < rate, rate, true
}

func forceCoreActiveOptions(itemOption int32) int {
	value := uint32(itemOption) & 0x0FFFFFFF
	active := 0
	for index := 0; index < forceCoreOptionSlots; index++ {
		core := (value >> uint(index*8)) & 0xFF
		code := core & forceCoreOptionMask
		if code == 0 || core&forceCoreEpicMask != 0 {
			continue
		}
		active += int((core & forceCoreOptionNumMask) >> 4)
	}
	return active
}

func ForceCoreHasRoom(itemOption int32) bool {
	maxSlots := int((uint32(itemOption) >> 28) & 0x0F)
	return maxSlots > 0 && forceCoreActiveOptions(itemOption) < maxSlots
}

// AddForceCoreOption adds one normal Force option to the packed item option.
func AddForceCoreOption(itemOption int32, optionCode int) (int32, bool) {
	if optionCode <= 0 || optionCode > 15 {
		return itemOption, false
	}

	value := uint32(itemOption) & 0x0FFFFFFF
	maxSlots := int((uint32(itemOption) >> 28) & 0x0F)
	if maxSlots == 0 || forceCoreActiveOptions(itemOption) >= maxSlots {
		return itemOption, false
	}

	for index := 0; index < forceCoreOptionSlots; index++ {
		shift := uint(index * 8)
		core := (value >> shift) & 0xFF
		code := int(core & forceCoreOptionMask)
		num := int((core & forceCoreOptionNumMask) >> 4)
		epics := core & forceCoreEpicMask
		if code == 0 {
			value = (value &^ (uint32(0xFF) << shift)) | (uint32(optionCode)|0x10)<<shift
			return int32(value | uint32(maxSlots)<<28), true
		}
		if code == optionCode && epics == 0 {
			if num >= 4 {
				return itemOption, false
			}
			core = (core &^ forceCoreOptionNumMask) | uint32(num+1)<<4
			value = (value &^ (uint32(0xFF) << shift)) | (core << shift)
			return int32(value | uint32(maxSlots)<<28), true
		}
	}

	return itemOption, false
}

func forceCoreClass(kind uint32) (string, bool) {
	itemType, ok := FindItemType(kind)
	if !ok {
		return "", false
	}

	switch itemType {
	case 3:
		// The local type contains the legacy necklace entry; the client
		// uses the same AmulLnk/AMULET option group for it.
		return "AMULET", true
	case 5, 6:
		// In this EP6 item.dec, type 5 is a magic sphere and type 6 is a
		// katana. Both use WepnLnk, the client's 1H Force Core group.
		return "1H", true
	case 7:
		return "2H", true
	case 8:
		return "SUIT", true
	case 9:
		return "GLOVE", true
	case 10:
		return "BOOT", true
	case 11:
		return "HELM", true
	case 15:
		return "RING", true
	case 16:
		return "AMULET", true
	case 17:
		return "EPULET", true
	case 22:
		return "BIKE", true
	case 32:
		return "EARRING", true
	case 33:
		return "BRACELET", true
	case 78:
		return "BELT", true
	default:
		return "", false
	}
}

// SelectForceCoreOption returns an option index, not a force code. The index
// is the value packed into the item's option bytes.
func SelectForceCoreOption(kind uint32) (int, bool) {
	itemClass, ok := forceCoreClass(kind)
	if !ok {
		return 0, false
	}
	candidates := forceCoreOptions[itemClass]
	if len(candidates) == 0 {
		return 0, false
	}

	limit := big.NewInt(int64(len(candidates)))
	selected, err := cryptorand.Int(cryptorand.Reader, limit)
	if err != nil {
		return 0, false
	}
	return candidates[selected.Int64()].Index, true
}

// SelectForceCoreSpecificOption converts a scroll's force code to the option
// index for the target item type. Two-handed targets use fcore_change_init.
func SelectForceCoreSpecificOption(kind uint32, scrollOption int32) (int, bool) {
	itemClass, ok := forceCoreClass(kind)
	if !ok {
		return 0, false
	}
	forceCode := int((uint32(scrollOption) & 0x000FFF80) >> 7)
	if forceCode <= 0 {
		return 0, false
	}
	if itemClass == "2H" {
		if changed, exists := forceCoreChanges[forceCode]; exists {
			forceCode = changed
		}
	}

	for _, option := range forceCoreOptions[itemClass] {
		if option.ForceCode == forceCode {
			return option.Index, true
		}
	}
	return 0, false
}
