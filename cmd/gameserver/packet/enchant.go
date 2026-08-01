package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	enchantResultOK    byte   = 1
	enchantResultError byte   = 6
	maxEnchantCores           = 7
	upgradeCoreShift          = 13
	upgradeCoreMask    uint32 = 0x0001E000
	maxUpgradeLevel    uint32 = 15
)

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
