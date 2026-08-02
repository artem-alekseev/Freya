package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
)

const (
	itemAttachResultOK    int32 = 0
	itemAttachResultError int32 = 1
)

// ItemAttach handles all client item-to-item operations. ArmorBinder remains
// the first resolver, while other source item types are dispatched from the
// client item data.
func ItemAttach(session *network.Session, reader *network.Reader) {
	sourceSlotValue := reader.ReadInt32()
	targetSlotValue := reader.ReadInt32()
	if sourceSlotValue < 0 || sourceSlotValue > int32(^uint16(0)) ||
		targetSlotValue < 0 || targetSlotValue > int32(^uint16(0)) ||
		sourceSlotValue == targetSlotValue {
		sendItemAttachResult(session, itemAttachResultError, inventory.Item{})
		return
	}

	sourceSlot := uint16(sourceSlotValue)
	targetSlot := uint16(targetSlotValue)
	ctx, err := context.Parse(session)
	if err != nil {
		log.Errorf("[ITEM_ATTACH] %s", err)
		sendItemAttachResult(session, itemAttachResultError, inventory.Item{})
		return
	}

	source := ctx.Char.Inventory.Get(sourceSlot)
	target := ctx.Char.Inventory.Get(targetSlot)
	newKind, ok := clientdata.FindArmorBinderResult(source.Kind, target.Kind)
	if ok {
		target.Kind = newKind
	} else {
		operation, supported := clientdata.FindItemAttachOperation(source.Kind)
		if !supported || operation != clientdata.ItemAttachSlotExtender {
			sendItemAttachResult(session, itemAttachResultError, inventory.Item{})
			return
		}

		if !applySlotExtender(&target) {
			sendItemAttachResult(session, itemAttachResultError, inventory.Item{})
			return
		}
	}

	ok, err = ctx.Char.Inventory.AttachWithTarget(sourceSlot, targetSlot, target)
	if err != nil {
		log.Errorf("[ITEM_ATTACH] character %d: %s", ctx.Char.Id, err)
	}
	if !ok {
		sendItemAttachResult(session, itemAttachResultError, inventory.Item{})
		return
	}

	sendItemAttachResult(session, itemAttachResultOK, target)
}

func applySlotExtender(target *inventory.Item) bool {
	if target == nil || target.Kind == 0 || !clientdata.IsSlotExtenderTarget(target.Kind) {
		return false
	}

	const (
		itemOwnershipMask uint32 = 0x00001000
		itemSealedMask    uint32 = 0x00100000
		optionSlotMask    uint32 = 0xF0000000
		optionValueMask   uint32 = 0x0FFFFFFF
		maxOptionSlots           = 4
		epicOptionMask    uint32 = 0x00000080
	)

	if target.Kind&itemOwnershipMask != 0 || target.Kind&itemSealedMask != 0 {
		return false
	}

	option := uint32(target.Option)
	slotCount := int((option & optionSlotMask) >> 28)
	limit := maxOptionSlots
	if option&epicOptionMask != 0 {
		limit--
	}
	if slotCount >= limit {
		return false
	}

	target.Option = int32((option & optionValueMask) | uint32(slotCount+1)<<28)
	target.Kind |= itemOwnershipMask
	return true
}

func sendItemAttachResult(session *network.Session, result int32, target inventory.Item) {
	pkt := network.NewWriter(ITEM_ATTACH)
	pkt.WriteInt32(result)
	pkt.WriteUint32(target.Kind)
	pkt.WriteUint32(target.Serials)
	pkt.WriteInt32(target.Option)
	pkt.WriteUint16(target.Slot)
	pkt.WriteUint32(target.Expire)
	session.Send(pkt)
}
