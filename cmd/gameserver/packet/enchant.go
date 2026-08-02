package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
)

const (
	enchantResultOK       byte   = 1
	enchantResultError    byte   = 6
	maxEnchantCores              = 7
	maxForceCoreCores            = 10
	enchantCorePacketSize        = 63
	upgradeCoreShift             = 13
	upgradeCoreMask       uint32 = 0x0001E000
	maxUpgradeLevel       uint32 = 15
)

// EnchantCore handles CSC_ENCHANTCORE (280). The client packet is the
// new-force-core layout from ProtosdefEx.h:
//
//	int targetSlotIdx
//	int randomScrollSlotIdx
//	int specificScrollSlotIdx
//	byte forceCoreNum
//	int forceCoreSlotIdx[10]
//
// The response is S2C_ENCHANTCORE: bool result, int itemOption.
//
// Do not route this packet through UpgradeCoreEnchant. That packet uses the
// legacy upgrade-core layout and changes the item kind, which corrupts the
// target item for clients sending opcode 280.
func EnchantCore(session *network.Session, reader *network.Reader) {
	if reader.Size != enchantCorePacketSize {
		log.Warningf("[ENCHANTCORE] invalid packet size: %d", reader.Size)
		sendEnchantCoreResult(session, false, 0)
		return
	}

	targetSlot := reader.ReadInt32()
	randomScrollSlot := reader.ReadInt32()
	specificScrollSlot := reader.ReadInt32()
	forceCoreNum := reader.ReadByte()
	forceCoreSlots := make([]int32, maxForceCoreCores)
	for i := range forceCoreSlots {
		forceCoreSlots[i] = reader.ReadInt32()
	}

	if !validEnchantInventorySlot(targetSlot) ||
		!validOptionalEnchantSlot(randomScrollSlot) ||
		!validOptionalEnchantSlot(specificScrollSlot) ||
		forceCoreNum == 0 || int(forceCoreNum) > maxForceCoreCores {
		log.Warningf("[ENCHANTCORE] invalid request: target=%d random=%d specific=%d cores=%d",
			targetSlot, randomScrollSlot, specificScrollSlot, forceCoreNum)
		sendEnchantCoreResult(session, false, 0)
		return
	}

	usedSlots := map[int32]struct{}{targetSlot: {}}
	if randomScrollSlot >= 0 {
		if _, exists := usedSlots[randomScrollSlot]; exists {
			sendEnchantCoreResult(session, false, 0)
			return
		}
		usedSlots[randomScrollSlot] = struct{}{}
	}
	if specificScrollSlot >= 0 {
		if _, exists := usedSlots[specificScrollSlot]; exists {
			sendEnchantCoreResult(session, false, 0)
			return
		}
		usedSlots[specificScrollSlot] = struct{}{}
	}

	for i := 0; i < int(forceCoreNum); i++ {
		slot := forceCoreSlots[i]
		if !validEnchantInventorySlot(slot) {
			log.Warningf("[ENCHANTCORE] invalid core slot: %d", slot)
			sendEnchantCoreResult(session, false, 0)
			return
		}
		if _, exists := usedSlots[slot]; exists {
			log.Warningf("[ENCHANTCORE] duplicated slot: %d", slot)
			sendEnchantCoreResult(session, false, 0)
			return
		}
		usedSlots[slot] = struct{}{}
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Errorf("[ENCHANTCORE] %s", err)
		sendEnchantCoreResult(session, false, 0)
		return
	}

	ctx.Mutex.RLock()
	character := ctx.Char
	if character == nil || character.Inventory == nil {
		ctx.Mutex.RUnlock()
		sendEnchantCoreResult(session, false, 0)
		return
	}
	target := character.Inventory.Get(uint16(targetSlot))
	var randomScroll inventory.Item
	if randomScrollSlot >= 0 {
		randomScroll = character.Inventory.Get(uint16(randomScrollSlot))
	}
	var specificScroll inventory.Item
	if specificScrollSlot >= 0 {
		specificScroll = character.Inventory.Get(uint16(specificScrollSlot))
	}
	cores := make([]inventory.Item, int(forceCoreNum))
	for i := range cores {
		cores[i] = character.Inventory.Get(uint16(forceCoreSlots[i]))
	}
	characterID := character.Id
	ctx.Mutex.RUnlock()

	if target.Kind == 0 || (randomScrollSlot >= 0 && randomScroll.Kind == 0) ||
		(specificScrollSlot >= 0 && specificScroll.Kind == 0) {
		log.Warningf("[ENCHANTCORE] missing target or scroll: character=%d target=%d random=%d specific=%d",
			characterID, targetSlot, randomScrollSlot, specificScrollSlot)
		sendEnchantCoreResult(session, false, 0)
		return
	}
	for i, core := range cores {
		if core.Kind == 0 {
			log.Warningf("[ENCHANTCORE] missing core: character=%d slot=%d index=%d",
				characterID, forceCoreSlots[i], i)
			sendEnchantCoreResult(session, false, 0)
			return
		}
	}

	if randomScrollSlot < 0 && specificScrollSlot < 0 {
		log.Warningf("[ENCHANTCORE] no scroll: character=%d target=%d", characterID, targetSlot)
		sendEnchantCoreResult(session, false, 0)
		return
	}
	if !clientdata.ForceCoreHasRoom(target.Option) {
		log.Warningf("[ENCHANTCORE] target has no free option slot: character=%d target=%d option=%d",
			characterID, targetSlot, target.Option)
		sendEnchantCoreResult(session, false, 0)
		return
	}

	success, rate, ok := clientdata.RollForceCoreSuccess(target.Kind, target.Option, forceCoreNum)
	if !ok {
		log.Warningf("[ENCHANTCORE] no fcore rate: character=%d target=%d option=%d cores=%d",
			characterID, targetSlot, target.Option, forceCoreNum)
		sendEnchantCoreResult(session, false, 0)
		return
	}

	newTarget := target
	if success {
		optionCode := 0
		if specificScrollSlot >= 0 {
			optionCode, ok = clientdata.SelectForceCoreSpecificOption(target.Kind, specificScroll.Option)
		} else {
			optionCode, ok = clientdata.SelectForceCoreOption(target.Kind)
		}
		if !ok {
			log.Warningf("[ENCHANTCORE] unable to select option: character=%d target=%d random=%d specific=%d",
				characterID, targetSlot, randomScrollSlot, specificScrollSlot)
			sendEnchantCoreResult(session, false, 0)
			return
		}
		newTarget.Option, ok = clientdata.AddForceCoreOption(target.Option, optionCode)
		if !ok {
			log.Warningf("[ENCHANTCORE] unable to add option: character=%d target=%d optionCode=%d",
				characterID, targetSlot, optionCode)
			sendEnchantCoreResult(session, false, 0)
			return
		}
	}

	var randomScrollPtr *inventory.Item
	if randomScrollSlot >= 0 {
		randomScrollPtr = &randomScroll
	}
	var specificScrollPtr *inventory.Item
	if specificScrollSlot >= 0 {
		specificScrollPtr = &specificScroll
	}

	ok, err = character.Inventory.ForceCoreEnchant(target, newTarget, randomScrollPtr, specificScrollPtr, cores)
	if err != nil {
		log.Errorf("[ENCHANTCORE] character %d: %s", characterID, err)
	}
	if !ok {
		sendEnchantCoreResult(session, false, 0)
		return
	}

	log.Debugf("[ENCHANTCORE] character=%d target=%d cores=%d rate=%d success=%t option=%d",
		characterID, targetSlot, forceCoreNum, rate, success, newTarget.Option)
	sendEnchantCoreResult(session, true, newTarget.Option)
}

func validEnchantInventorySlot(slot int32) bool {
	return slot >= 0 && slot <= int32(^uint16(0))
}

func validOptionalEnchantSlot(slot int32) bool {
	return slot == -1 || validEnchantInventorySlot(slot)
}

func sendEnchantCoreResult(session *network.Session, result bool, itemOption int32) {
	pkt := network.NewWriter(ENCHANT_CORE)
	pkt.WriteBool(result)
	pkt.WriteInt32(itemOption)
	session.Send(pkt)
}

// UpgradeCoreEnchant handles CSC_UPGRADECOREENCHANT. The EP6 request is a
// fixed-size packet: core count, target inventory slot, then seven WORD core
// slots. Only the first coreCount entries are used.
func UpgradeCoreEnchant(session *network.Session, reader *network.Reader) {
	coreCount := reader.ReadByte()
	targetSlot := reader.ReadUint16()

	if coreCount == 0 || coreCount > maxEnchantCores {
		sendEnchantResult(session, enchantResultError, 0, 0, 0)
		return
	}

	coreSlots := make([]uint16, coreCount)
	for i := range coreSlots {
		coreSlots[i] = reader.ReadUint16()
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error("[UPGRADECOREENCHANT]", err)
		sendEnchantResult(session, enchantResultError, 0, 0, 0)
		return
	}

	target := ctx.Char.Inventory.Get(targetSlot)
	newKind, ok := nextEnchantKind(target.Kind)
	if !ok {
		sendEnchantResult(session, enchantResultError, 0, 0, 0)
		return
	}

	ok, err = ctx.Char.Inventory.Enchant(targetSlot, newKind, coreSlots)
	if err != nil {
		log.Errorf("[UPGRADECOREENCHANT] character %d: %s", ctx.Char.Id, err)
	}
	if !ok {
		sendEnchantResult(session, enchantResultError, 0, 0, 0)
		return
	}

	target.Kind = newKind
	sendEnchantResult(session, enchantResultOK, target.Kind, target.Option, target.Expire)
}

func nextEnchantKind(kind uint32) (uint32, bool) {
	if kind == 0 {
		return 0, false
	}

	level := (kind & upgradeCoreMask) >> upgradeCoreShift
	if level >= maxUpgradeLevel {
		return 0, false
	}

	return (kind &^ upgradeCoreMask) | ((level + 1) << upgradeCoreShift), true
}

func sendEnchantResult(session *network.Session, result byte, kind uint32, option int32, expire uint32) {
	pkt := network.NewWriter(UPGRADE_CORE_ENCHANT)
	pkt.WriteByte(result)
	pkt.WriteUint32(kind)
	pkt.WriteInt32(option)
	pkt.WriteUint32(expire)
	session.Send(pkt)
}
