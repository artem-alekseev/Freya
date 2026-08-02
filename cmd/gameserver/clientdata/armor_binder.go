package clientdata

import "encoding/binary"

const (
	// 103 is the ArmorBinder item type stored in the current item.dec. Item
	// IDs are discovered by scanning the file and are never embedded here.
	armorBinderItemType     uint32 = 103
	itemClassOffset                = 276
	itemMaterialOffset             = 396
	armorBinderClassOffset0        = 352
	armorBinderClassOffset1        = 360
	armorBinderClassOffset2        = 368
)

type armorVariantSignature struct {
	itemType uint32
	field264 uint32
	field268 uint32
	field284 uint32
	field392 uint32
	field396 uint32
}

type armorBinderInfo struct {
	material byte
	classes  []byte
}

type armorBinderKey struct {
	source uint32
	target uint32
}

var armorBinderResults = make(map[armorBinderKey]uint32)

// parseArmorBinderData builds the ArmorBinder conversion table from item.dec.
// A base armor record is recognized by a matching record with a non-zero
// class field and the same item data signature. This keeps the conversion
// generic for every material, armor part and class present in the client data.
func parseArmorBinderData(data []byte, layout itemFileLayout) map[armorBinderKey]uint32 {
	results := make(map[armorBinderKey]uint32)
	if layout.recordSize < itemMaterialOffset+4 {
		return results
	}

	binders := make(map[uint32]armorBinderInfo)
	bases := make(map[armorVariantSignature]uint32)
	variants := make(map[armorVariantSignature]map[byte]uint32)

	for itemID, recordStart := uint32(1), layout.headerSize; recordStart+layout.recordSize <= len(data); itemID, recordStart = itemID+1, recordStart+layout.recordSize {
		record := data[recordStart : recordStart+layout.recordSize]
		itemType := binary.LittleEndian.Uint32(record[itemTypeOffset:])
		itemIndex := itemID & itemKindIndexMask
		itemClass := byte(binary.LittleEndian.Uint32(record[itemClassOffset:]))
		material := byte((binary.LittleEndian.Uint32(record[itemMaterialOffset:]) >> 8) & 0xff)

		if itemType == armorBinderItemType {
			classes := make([]byte, 0, 3)
			for _, offset := range []int{
				armorBinderClassOffset0,
				armorBinderClassOffset1,
				armorBinderClassOffset2,
			} {
				class := byte(binary.LittleEndian.Uint32(record[offset:]) >> 24)
				if class == 0 {
					continue
				}
				alreadyAdded := false
				for _, existing := range classes {
					if existing == class {
						alreadyAdded = true
						break
					}
				}
				if !alreadyAdded {
					classes = append(classes, class)
				}
			}
			if len(classes) > 0 {
				binders[itemIndex] = armorBinderInfo{
					material: material,
					classes:  classes,
				}
			}
			continue
		}

		signature := armorVariantSignature{
			itemType: itemType,
			field264: binary.LittleEndian.Uint32(record[264:]),
			field268: binary.LittleEndian.Uint32(record[268:]),
			field284: binary.LittleEndian.Uint32(record[284:]),
			field392: binary.LittleEndian.Uint32(record[392:]),
			field396: binary.LittleEndian.Uint32(record[396:]),
		}

		if itemClass == 0 {
			if _, exists := bases[signature]; !exists {
				bases[signature] = itemIndex
			}
			continue
		}

		if variants[signature] == nil {
			variants[signature] = make(map[byte]uint32)
		}
		if _, exists := variants[signature][itemClass]; !exists {
			variants[signature][itemClass] = itemIndex
		}
	}

	for signature, targetIndex := range bases {
		classVariants := variants[signature]
		if len(classVariants) == 0 {
			continue
		}
		targetMaterial := byte((signature.field396 >> 8) & 0xff)
		for sourceIndex, binder := range binders {
			if binder.material != targetMaterial {
				continue
			}
			for _, class := range binder.classes {
				if resultIndex, exists := classVariants[class]; exists {
					results[armorBinderKey{source: sourceIndex, target: targetIndex}] = resultIndex
				}
			}
		}
	}

	return results
}

// FindArmorBinderResult returns the target kind after applying a compatible
// ArmorBinder. Upgrade/ownership bits on the target kind are preserved while
// only the item index is replaced with the class-specific client record.
func FindArmorBinderResult(sourceKind, targetKind uint32) (uint32, bool) {
	sourceIndex := sourceKind & itemKindIndexMask
	targetIndex := targetKind & itemKindIndexMask
	resultIndex, ok := armorBinderResults[armorBinderKey{source: sourceIndex, target: targetIndex}]
	if !ok {
		return 0, false
	}

	return (targetKind &^ itemKindIndexMask) | resultIndex, true
}
