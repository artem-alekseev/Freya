package clientdata

import "encoding/binary"

// ItemAttachOperation is selected from the item type stored in item.dec.
// Item IDs are intentionally not embedded here: different client data sets
// can contain different concrete extender and binder items.
type ItemAttachOperation byte

const (
	ItemAttachUnsupported ItemAttachOperation = iota
	ItemAttachSlotExtender
	ItemAttachArmorBinder
)

const (
	itemAttachSlotExtenderType uint32 = 29
	itemAttachTargetTypeMin    uint32 = 5
	itemAttachTargetTypeMax    uint32 = 11
)

var itemTypes = make(map[uint32]uint32)

func parseItemTypes(data []byte, layout itemFileLayout) map[uint32]uint32 {
	types := make(map[uint32]uint32)
	if layout.recordSize < itemTypeOffset+4 {
		return types
	}

	for itemID, recordStart := uint32(1), layout.headerSize; recordStart+layout.recordSize <= len(data); itemID, recordStart = itemID+1, recordStart+layout.recordSize {
		types[itemID&itemDataKindMask] = binary.LittleEndian.Uint32(data[recordStart+itemTypeOffset : recordStart+itemTypeOffset+4])
	}

	return types
}

// FindItemType returns the client-data type for an item kind, without its
// upgrade/instance bits.
func FindItemType(kind uint32) (uint32, bool) {
	itemType, ok := itemTypes[kind&itemDataKindMask]
	return itemType, ok
}

// FindItemAttachOperation resolves the operation by the source item's type.
func FindItemAttachOperation(kind uint32) (ItemAttachOperation, bool) {
	itemType, ok := FindItemType(kind)
	if !ok {
		return ItemAttachUnsupported, false
	}

	switch itemType {
	case itemAttachSlotExtenderType:
		return ItemAttachSlotExtender, true
	case armorBinderItemType:
		return ItemAttachArmorBinder, true
	default:
		return ItemAttachUnsupported, false
	}
}

// IsSlotExtenderTarget reports whether the current client data describes an
// equipment item that can have option slots. The concrete item IDs remain
// data-driven; the range is the equipment type range used by this client.
func IsSlotExtenderTarget(kind uint32) bool {
	itemType, ok := FindItemType(kind)
	return ok && itemType >= itemAttachTargetTypeMin && itemType <= itemAttachTargetTypeMax
}
