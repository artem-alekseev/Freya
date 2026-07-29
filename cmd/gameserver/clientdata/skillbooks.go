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
	skillBookPriceOffset = 327
	skillBookSkillOffset = 339
	skillBookItemType    = 77
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

type Stats struct {
	Worlds  int
	Entries int
	Books   int
}

type trainerKey struct {
	worldID byte
	trainer byte
}

var skillBooks = make(map[trainerKey]map[uint16]SkillBook)
var skillBooksByItem = make(map[uint32]SkillBook)

func Initialize(directory string) (Stats, error) {
	itemData, err := os.ReadFile(filepath.Join(directory, "item.dec"))
	if err != nil {
		return Stats{}, fmt.Errorf("read item.dec: %w", err)
	}

	itemLayout, err := detectItemLayout(itemData)
	if err != nil {
		return Stats{}, err
	}

	cabalData, err := os.ReadFile(filepath.Join(directory, "cabal.dec"))
	if err != nil {
		return Stats{}, fmt.Errorf("read cabal.dec: %w", err)
	}

	loaded, stats, err := parseSkillBooks(cabalData, itemData, itemLayout)
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

func parseSkillBooks(cabalData, itemData []byte, layout itemFileLayout) (map[trainerKey]map[uint16]SkillBook, Stats, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, Stats{}, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	loaded := make(map[trainerKey]map[uint16]SkillBook)
	uniqueBooks := make(map[uint32]struct{})

	var inCabalWorld bool
	var worldID byte
	var trainer byte

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Stats{}, fmt.Errorf("parse cabal.dec XML: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "cabal_world":
				inCabalWorld = true
			case "world":
				if inCabalWorld {
					value, err := uintAttribute(element, "id", 8)
					if err != nil {
						return nil, Stats{}, err
					}
					worldID = byte(value)
				}
			case "trainer":
				if inCabalWorld && worldID != 0 {
					value, err := uintAttribute(element, "id", 8)
					if err != nil {
						return nil, Stats{}, err
					}
					trainer = byte(value)
				}
			case "skill":
				if inCabalWorld && worldID != 0 && trainer != 0 {
					book, err := parseSkillBook(element, worldID, trainer, itemData, layout)
					if err != nil {
						return nil, Stats{}, err
					}

					key := trainerKey{worldID: worldID, trainer: trainer}
					if loaded[key] == nil {
						loaded[key] = make(map[uint16]SkillBook)
					}
					if _, exists := loaded[key][book.Slot]; exists {
						return nil, Stats{}, fmt.Errorf("duplicate trainer %d skill slot %d in world %d", trainer, book.Slot, worldID)
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

	stats := Stats{Worlds: len(loaded), Books: len(uniqueBooks)}
	for _, books := range loaded {
		stats.Entries += len(books)
	}
	if stats.Entries == 0 {
		return nil, Stats{}, fmt.Errorf("cabal.dec does not contain trainer skill books")
	}

	return loaded, stats, nil
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
