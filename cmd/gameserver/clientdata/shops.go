package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
)

type ShopItem struct {
	// Kind is the complete item kind index written to the character
	// inventory. Older cabal.dec files call this attribute item_id, while
	// newer files may expose it as kind/kind_idx.
	Kind uint32

	// ItemID is kept as a compatibility alias for callers that still use the
	// old name. It has the same value as Kind after parsing.
	ItemID uint32
	Option int32
	Price  uint64
}

type shopKey struct {
	worldID byte
	npcID   byte
}

var shopItems = make(map[shopKey]map[int32]ShopItem)

func FindShopItem(worldID, npcID byte, slot int32) (ShopItem, bool) {
	items, ok := shopItems[shopKey{worldID: worldID, npcID: npcID}]
	if !ok {
		return ShopItem{}, false
	}

	item, ok := items[slot]
	return item, ok
}

func parseShops(cabalData []byte) (map[shopKey]map[int32]ShopItem, error) {
	xmlStart := bytes.Index(cabalData, []byte("<cabal>"))
	if xmlStart < 0 {
		return nil, fmt.Errorf("cabal.dec does not contain a cabal XML section")
	}

	decoder := xml.NewDecoder(bytes.NewReader(cabalData[xmlStart:]))
	loaded := make(map[shopKey]map[int32]ShopItem)

	var inCabalWorld bool
	var worldID, shopID byte

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
			case "cabal_world":
				inCabalWorld = true
			case "world":
				if inCabalWorld {
					value, err := uintAttribute(element, "id", 8)
					if err != nil {
						return nil, err
					}
					worldID = byte(value)
				}
			case "shop":
				if inCabalWorld && worldID != 0 {
					value, err := uintAttribute(element, "id", 8)
					if err != nil {
						return nil, err
					}
					shopID = byte(value)
				}
			case "item":
				if inCabalWorld && worldID != 0 && shopID != 0 {
					slot, item, err := parseShopItem(element)
					if err != nil {
						return nil, err
					}
					key := shopKey{worldID: worldID, npcID: shopID}
					if loaded[key] == nil {
						loaded[key] = make(map[int32]ShopItem)
					}
					if _, exists := loaded[key][slot]; exists {
						return nil, fmt.Errorf("duplicate shop item in world %d NPC %d slot %d", worldID, shopID, slot)
					}
					loaded[key][slot] = item
				}
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "shop":
				shopID = 0
			case "world":
				if inCabalWorld {
					worldID = 0
				}
			case "cabal_world":
				inCabalWorld = false
			}
		}
	}

	if len(loaded) == 0 {
		return nil, fmt.Errorf("cabal.dec does not contain NPC shops")
	}

	return loaded, nil
}

func parseShopItem(element xml.StartElement) (int32, ShopItem, error) {
	slot, err := intAttribute(element, "slot_id", 32)
	if err != nil {
		return 0, ShopItem{}, err
	}

	itemKind, found, err := optionalUintAttribute(element, "kind", 32)
	if err != nil {
		return 0, ShopItem{}, err
	}
	if !found {
		itemKind, found, err = optionalUintAttribute(element, "kind_idx", 32)
		if err != nil {
			return 0, ShopItem{}, err
		}
	}
	if !found {
		itemKind, err = uintAttribute(element, "item_id", 32)
		if err != nil {
			return 0, ShopItem{}, err
		}
	}
	option, err := intAttribute(element, "option", 32)
	if err != nil {
		return 0, ShopItem{}, err
	}
	price, err := uintAttribute(element, "price", 64)
	if err != nil {
		return 0, ShopItem{}, err
	}
	if slot < 0 {
		return 0, ShopItem{}, fmt.Errorf("invalid NPC shop item in slot %d", slot)
	}

	return int32(slot), ShopItem{
		Kind:   uint32(itemKind),
		ItemID: uint32(itemKind),
		Option: int32(option),
		Price:  price,
	}, nil
}

func shopItemCount(shops map[shopKey]map[int32]ShopItem) int {
	count := 0
	for _, items := range shops {
		count += len(items)
	}
	return count
}
