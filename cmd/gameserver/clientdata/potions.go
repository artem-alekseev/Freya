package clientdata

import "encoding/binary"

const (
	itemDataKindMask     uint32 = 0x00000FFF
	itemPotionTypeOffset        = 339
	itemPotionRecordType uint32 = 14
	itemPotionManaType   byte   = 2
)

var manaPotionData = make(map[uint32]uint16)

// The recovery values are the values shown in the client descriptions. The
// nearby numeric fields in item.dec are item prices, not potion recovery.
var manaPotionRecovery = map[uint32]uint16{
	6:    50,
	7:    200,
	8:    400,
	1010: 400,
	1116: 400,
}

func parseManaPotionData(data []byte, layout itemFileLayout) map[uint32]uint16 {
	potions := make(map[uint32]uint16)
	if layout.recordSize <= itemPotionTypeOffset {
		return potions
	}

	for itemID, recordStart := uint32(1), layout.headerSize; recordStart+layout.recordSize <= len(data); itemID, recordStart = itemID+1, recordStart+layout.recordSize {
		itemType := binary.LittleEndian.Uint32(data[recordStart+itemTypeOffset : recordStart+itemTypeOffset+4])
		if itemType != itemPotionRecordType {
			continue
		}
		if data[recordStart+itemPotionTypeOffset] != itemPotionManaType {
			continue
		}

		if recovery, ok := manaPotionRecovery[itemID&itemDataKindMask]; ok {
			potions[itemID&itemDataKindMask] = recovery
		}
	}

	return potions
}

// FindManaPotionRecovery returns the MP recovery used by the client for the
// supported mana potions.
func FindManaPotionRecovery(itemID uint32) (uint16, bool) {
	recovery, ok := manaPotionData[itemID&itemDataKindMask]
	return recovery, ok
}
