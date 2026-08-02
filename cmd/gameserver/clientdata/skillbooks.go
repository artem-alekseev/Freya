package clientdata

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

const (
	itemNameOffset       = 64
	itemTypeOffset       = 204
	itemSellPriceOffset  = 327
	skillBookPriceOffset = itemSellPriceOffset
	skillBookSkillOffset = 339
	skillBookItemType    = 77

	SkillTrainByQuest byte = 11
)

type SkillBook struct {
	WorldID byte
	Trainer byte
	Slot    uint16
	SkillID uint16
	Level   byte
	ItemID  uint32
	Price   uint32
}

type SkillLevel struct {
	UntrainPrice uint64
	TrainType    byte
}

type Stats struct {
	Worlds          int
	Entries         int
	Books           int
	SkillLevels     int
	CharacterLevels int
	ShopItems       int
	WarpPoints      int
}

type trainerKey struct {
	worldID byte
	trainer byte
}

var skillBooks = make(map[trainerKey]map[uint16]SkillBook)
var skillBooksByItem = make(map[uint32]SkillBook)
var skillLevels = make(map[skillLevelKey]SkillLevel)
var itemSellData = make(map[uint32]itemSellInfo)

type itemSellInfo struct {
	price    uint64
	itemType uint32
}

type skillLevelKey struct {
	skillID uint16
	level   byte
}

func Initialize(directory string) (Stats, error) {
	itemData, err := os.ReadFile(filepath.Join(directory, "item.dec"))
	if err != nil {
		return Stats{}, fmt.Errorf("read item.dec: %w", err)
	}

	itemLayout, err := detectItemLayout(itemData)
	if err != nil {
		return Stats{}, err
	}
	parsedItemTypes := parseItemTypes(itemData, itemLayout)
	sellData, err := parseItemSellPrices(itemData, itemLayout)
	if err != nil {
		return Stats{}, err
	}
	parsedManaPotions := parseManaPotionData(itemData, itemLayout)
	parsedArmorBinderData := parseArmorBinderData(itemData, itemLayout)

	cazData, err := os.ReadFile(filepath.Join(directory, "caz.dec"))
	if err != nil {
		return Stats{}, fmt.Errorf("read caz.dec: %w", err)
	}
	parsedItemDurations, err := parseItemDurations(cazData)
	if err != nil {
		return Stats{}, err
	}

	cabalData, err := os.ReadFile(filepath.Join(directory, "cabal.dec"))
	if err != nil {
		return Stats{}, fmt.Errorf("read cabal.dec: %w", err)
	}
	questData, err := os.ReadFile(filepath.Join(directory, "quest.dec"))
	if err != nil {
		return Stats{}, fmt.Errorf("read quest.dec: %w", err)
	}
	parsedQuestMissionCounts, err := parseQuestMissionCounts(questData)
	if err != nil {
		return Stats{}, err
	}
	parsedQuestNPCActions, err := parseQuestNPCActions(questData)
	if err != nil {
		return Stats{}, err
	}
	parsedQuestMissionItems, err := parseQuestMissionItems(questData)
	if err != nil {
		return Stats{}, err
	}
	parsedQuestRewards, err := parseQuestRewards(questData)
	if err != nil {
		return Stats{}, err
	}
	parsedWarpPoints, err := parseWarpPoints(cabalData)
	if err != nil {
		return Stats{}, err
	}
	parsedMapWarps, err := parseMapWarpIndices(cabalData)
	if err != nil {
		return Stats{}, err
	}
	parsedWarpRoutes, err := parseWarpRoutes(cabalData)
	if err != nil {
		return Stats{}, err
	}
	damageData, err := parseSkillDamageData(cabalData)
	if err != nil {
		return Stats{}, err
	}
	skillMetadata, err := parseSkillMetaData(cabalData)
	if err != nil {
		return Stats{}, err
	}
	skillBuffs, err := parseSkillBuffData(cabalData)
	if err != nil {
		return Stats{}, err
	}
	weaponUpgradeFormulas, err := parseWeaponUpgradeFormulas(cabalData)
	if err != nil {
		return Stats{}, err
	}
	parsedWeaponData := parseWeaponAttackData(itemData, itemLayout, weaponUpgradeFormulas)
	if len(parsedWeaponData) == 0 {
		return Stats{}, fmt.Errorf("item.dec does not contain weapon attack values")
	}

	loaded, levels, stats, err := parseSkillBooks(cabalData, itemData, itemLayout)
	if err != nil {
		return Stats{}, err
	}
	shops, err := parseShops(cabalData)
	if err != nil {
		return Stats{}, err
	}
	levelsTable, err := parseCharacterLevels(cabalData)
	if err != nil {
		return Stats{}, err
	}
	rankProgress, rankBonuses, err := parseSkillRankData(cabalData)
	if err != nil {
		return Stats{}, err
	}
	rankLimits, err := parseSkillRankLimitData(cabalData)
	if err != nil {
		return Stats{}, err
	}
	battleModeSkills, err := parseBattleModeSkills(cabalData)
	if err != nil {
		return Stats{}, err
	}
	parsedForceCoreRates, parsedForceCoreOptions, parsedForceCoreChanges, err := parseForceCoreData(cabalData)
	if err != nil {
		return Stats{}, err
	}
	byItem := make(map[uint32]SkillBook)
	for _, trainerBooks := range loaded {
		for _, book := range trainerBooks {
			if existing, ok := byItem[book.ItemID]; ok {
				if existing.SkillID != book.SkillID || existing.Level != book.Level || existing.Price != book.Price {
					return Stats{}, fmt.Errorf("conflicting trainer data for skill book item %d", book.ItemID)
				}
				continue
			}
			byItem[book.ItemID] = book
		}
	}

	skillBooks = loaded
	skillBooksByItem = byItem
	skillLevels = levels
	skillDamageData = damageData
	skillMetaData = skillMetadata
	skillBuffData = skillBuffs
	itemSellData = sellData
	itemTypes = parsedItemTypes
	manaPotionData = parsedManaPotions
	armorBinderResults = parsedArmorBinderData
	itemDurations = parsedItemDurations
	if len(parsedWeaponData) > 0 {
		weaponAttackData = parsedWeaponData
	}
	characterLevels = levelsTable
	skillRankProgressData = rankProgress
	skillRankBonusData = rankBonuses
	skillRankLimitData = rankLimits
	battleModeSkillData = battleModeSkills
	forceCoreRates = parsedForceCoreRates
	forceCoreOptions = parsedForceCoreOptions
	forceCoreChanges = parsedForceCoreChanges
	shopItems = shops
	questMissionCounts = parsedQuestMissionCounts
	questNPCActions = parsedQuestNPCActions
	questMissionItems = parsedQuestMissionItems
	questRewards = parsedQuestRewards
	stats.CharacterLevels = len(levelsTable)
	stats.ShopItems = shopItemCount(shops)
	warpPoints = parsedWarpPoints
	warpRoutes = parsedWarpRoutes
	mapWarps = parsedMapWarps
	stats.WarpPoints = len(parsedWarpPoints)
	return stats, nil
}

func FindSkillBook(worldID, trainer byte, slot uint16) (SkillBook, bool) {
	books, ok := skillBooks[trainerKey{worldID: worldID, trainer: trainer}]
	if !ok {
		return SkillBook{}, false
	}

	book, ok := books[slot]
	return book, ok
}

func FindSkillBookItem(itemID uint32) (SkillBook, bool) {
	book, ok := skillBooksByItem[itemID]
	return book, ok
}

func FindItemSellPrice(itemID uint32) (uint64, bool) {
	data, ok := itemSellData[itemID&itemDataKindMask]
	return data.price, ok
}

func FindItemSellValue(itemID uint32, itemOption int32) (uint64, bool) {
	data, ok := itemSellData[itemID&itemDataKindMask]
	if !ok || data.price == 0 {
		return 0, false
	}

	count := uint64(1)
	switch data.itemType {
	case 16, 27, 102: // QSTS, SPOS, FCTL: count is in the low 16 bits.
		count = uint64(uint32(itemOption) & 0xFFFF)
	case 67, 70: // EVTS, FONT: count is also kept in the low option bits.
		count = uint64(uint32(itemOption) & 0xFFFF)
	case 13, 15, 42, 46, 60, 71, 76, 80, 81, 83, 87, 88, 93, 95, 96:
		// Other stackable types use the complete item option as their count.
		count = uint64(uint32(itemOption))
	}
	if count == 0 || ^uint64(0)/data.price < count {
		return 0, false
	}

	return data.price * count, true
}

func FindSkillLevel(skillID uint16, level byte) (SkillLevel, bool) {
	info, ok := skillLevels[skillLevelKey{skillID: skillID, level: level}]
	return info, ok
}

type itemFileLayout struct {
	headerSize int
	recordSize int
}

func detectItemLayout(data []byte) (itemFileLayout, error) {
	firstName := bytes.Index(data, append([]byte("item1"), 0))
	secondName := bytes.Index(data, append([]byte("item2"), 0))
	if firstName < itemNameOffset || secondName <= firstName {
		return itemFileLayout{}, fmt.Errorf("item.dec has an unsupported record layout")
	}

	layout := itemFileLayout{
		headerSize: firstName - itemNameOffset,
		recordSize: secondName - firstName,
	}
	if layout.recordSize <= skillBookSkillOffset+4 {
		return itemFileLayout{}, fmt.Errorf("item.dec record is too short: %d bytes", layout.recordSize)
	}

	return layout, nil
}

func parseItemSellPrices(data []byte, layout itemFileLayout) (map[uint32]itemSellInfo, error) {
	prices := make(map[uint32]itemSellInfo)
	for itemID, recordStart := uint32(1), layout.headerSize; recordStart+layout.recordSize <= len(data); itemID, recordStart = itemID+1, recordStart+layout.recordSize {
		priceOffset := recordStart + itemSellPriceOffset
		if priceOffset+4 > recordStart+layout.recordSize {
			return nil, fmt.Errorf("item %d record does not contain sell price", itemID)
		}

		price := binary.LittleEndian.Uint32(data[priceOffset : priceOffset+4])
		if price != 0 {
			itemType := binary.LittleEndian.Uint32(data[recordStart+itemTypeOffset : recordStart+itemTypeOffset+4])
			prices[itemID] = itemSellInfo{price: uint64(price), itemType: itemType}
		}
	}

	if len(prices) == 0 {
		return nil, fmt.Errorf("item.dec does not contain item sell prices")
	}
	return prices, nil
}

func parseSkillBooks(cabalData, itemData []byte, layout itemFileLayout) (
	map[trainerKey]map[uint16]SkillBook,
	map[skillLevelKey]SkillLevel,
	Stats,
	error,
) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, nil, Stats{}, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	loaded := make(map[trainerKey]map[uint16]SkillBook)
	uniqueBooks := make(map[uint32]struct{})
	levels := make(map[skillLevelKey]SkillLevel)

	var inCabalWorld bool
	var worldID byte
	var trainer byte

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, Stats{}, fmt.Errorf("parse cabal.dec XML: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "skill_order":
				skillID, startLevel, endLevel, info, err := parseSkillLevel(element)
				if err != nil {
					return nil, nil, Stats{}, err
				}
				for level := int(startLevel); level <= int(endLevel); level++ {
					key := skillLevelKey{skillID: skillID, level: byte(level)}
					if existing, ok := levels[key]; ok && existing != info {
						return nil, nil, Stats{}, fmt.Errorf(
							"conflicting skill order for skill %d level %d", skillID, level)
					}
					levels[key] = info
				}
			case "cabal_world":
				inCabalWorld = true
			case "world":
				if inCabalWorld {
					value, err := uintAttribute(element, "id", 8)
					if err != nil {
						return nil, nil, Stats{}, err
					}
					worldID = byte(value)
				}
			case "trainer":
				if inCabalWorld && worldID != 0 {
					value, err := uintAttribute(element, "id", 8)
					if err != nil {
						return nil, nil, Stats{}, err
					}
					trainer = byte(value)
				}
			case "skill":
				if inCabalWorld && worldID != 0 && trainer != 0 {
					book, err := parseSkillBook(element, worldID, trainer, itemData, layout)
					if err != nil {
						return nil, nil, Stats{}, err
					}

					key := trainerKey{worldID: worldID, trainer: trainer}
					if loaded[key] == nil {
						loaded[key] = make(map[uint16]SkillBook)
					}
					if _, exists := loaded[key][book.Slot]; exists {
						return nil, nil, Stats{}, fmt.Errorf(
							"duplicate trainer %d skill slot %d in world %d", trainer, book.Slot, worldID)
					}

					loaded[key][book.Slot] = book
					uniqueBooks[book.ItemID] = struct{}{}
				}
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "trainer":
				trainer = 0
			case "world":
				if inCabalWorld {
					worldID = 0
				}
			case "cabal_world":
				inCabalWorld = false
			}
		}
	}

	stats := Stats{Worlds: len(loaded), Books: len(uniqueBooks), SkillLevels: len(levels)}
	for _, books := range loaded {
		stats.Entries += len(books)
	}
	if stats.Entries == 0 {
		return nil, nil, Stats{}, fmt.Errorf("cabal.dec does not contain trainer skill books")
	}
	if stats.SkillLevels == 0 {
		return nil, nil, Stats{}, fmt.Errorf("cabal.dec does not contain skill orders")
	}

	return loaded, levels, stats, nil
}

func parseSkillLevel(element xml.StartElement) (uint16, byte, byte, SkillLevel, error) {
	skillID, err := uintAttribute(element, "id", 16)
	if err != nil {
		return 0, 0, 0, SkillLevel{}, err
	}
	startLevel, err := uintAttribute(element, "start_level", 8)
	if err != nil {
		return 0, 0, 0, SkillLevel{}, err
	}
	endLevel, err := uintAttribute(element, "end_level", 8)
	if err != nil {
		return 0, 0, 0, SkillLevel{}, err
	}
	trainType, err := uintAttribute(element, "train_type", 8)
	if err != nil {
		return 0, 0, 0, SkillLevel{}, err
	}
	untrainPrice, err := uintAttribute(element, "untrain_price", 64)
	if err != nil {
		return 0, 0, 0, SkillLevel{}, err
	}
	if skillID == 0 || startLevel == 0 || endLevel < startLevel {
		return 0, 0, 0, SkillLevel{}, fmt.Errorf(
			"invalid skill order for skill %d levels %d-%d", skillID, startLevel, endLevel)
	}

	return uint16(skillID), byte(startLevel), byte(endLevel), SkillLevel{
		UntrainPrice: untrainPrice,
		TrainType:    byte(trainType),
	}, nil
}

func parseSkillBook(element xml.StartElement, worldID, trainer byte, itemData []byte, layout itemFileLayout) (SkillBook, error) {
	slot, err := uintAttribute(element, "slot_id", 16)
	if err != nil {
		return SkillBook{}, err
	}
	skillID, err := uintAttribute(element, "id", 16)
	if err != nil {
		return SkillBook{}, err
	}
	level, err := uintAttribute(element, "level", 8)
	if err != nil {
		return SkillBook{}, err
	}
	itemID, err := uintAttribute(element, "skill_book", 32)
	if err != nil {
		return SkillBook{}, err
	}
	if itemID == 0 {
		return SkillBook{}, fmt.Errorf("trainer %d skill %d has no skill book", trainer, skillID)
	}

	recordStart := layout.headerSize + (int(itemID)-1)*layout.recordSize
	recordEnd := recordStart + layout.recordSize
	if recordStart < 0 || recordEnd > len(itemData) {
		return SkillBook{}, fmt.Errorf("item.dec does not contain skill book item %d", itemID)
	}

	itemType := binary.LittleEndian.Uint32(itemData[recordStart+itemTypeOffset:])
	if itemType != skillBookItemType {
		return SkillBook{}, fmt.Errorf("item %d has type %d instead of skill book type %d", itemID, itemType, skillBookItemType)
	}

	linkedSkill := binary.LittleEndian.Uint32(itemData[recordStart+skillBookSkillOffset:])
	if linkedSkill != uint32(skillID) {
		return SkillBook{}, fmt.Errorf("skill book item %d links to skill %d instead of %d", itemID, linkedSkill, skillID)
	}

	price := binary.LittleEndian.Uint32(itemData[recordStart+skillBookPriceOffset:])
	if price == 0 {
		return SkillBook{}, fmt.Errorf("skill book item %d has no price", itemID)
	}

	return SkillBook{
		WorldID: worldID,
		Trainer: trainer,
		Slot:    uint16(slot),
		SkillID: uint16(skillID),
		Level:   byte(level),
		ItemID:  uint32(itemID),
		Price:   price,
	}, nil
}

func uintAttribute(element xml.StartElement, name string, bitSize int) (uint64, error) {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != name {
			continue
		}

		value, err := strconv.ParseUint(attribute.Value, 10, bitSize)
		if err != nil {
			return 0, fmt.Errorf("invalid %s attribute %q on <%s>: %w", name, attribute.Value, element.Name.Local, err)
		}
		return value, nil
	}

	return 0, fmt.Errorf("missing %s attribute on <%s>", name, element.Name.Local)
}

func intAttribute(element xml.StartElement, name string, bitSize int) (int64, error) {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != name {
			continue
		}

		value, err := strconv.ParseInt(attribute.Value, 10, bitSize)
		if err != nil {
			return 0, fmt.Errorf("invalid %s attribute %q on <%s>: %w", name, attribute.Value, element.Name.Local, err)
		}
		return value, nil
	}

	return 0, fmt.Errorf("missing %s attribute on <%s>", name, element.Name.Local)
}
