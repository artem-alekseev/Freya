package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/network"
)

type StorageType int

const (
	Inventory StorageType = iota
	Equipment
)

func notifyStorageExchange(session *network.Session, result bool) {
	packet := network.NewWriter(STORAGE_EXCHANGE_MOVE)
	packet.WriteBool(result)
	packet.WriteInt32(0)

	session.Send(packet)
}

func StorageExchangeMove(session *network.Session, reader *network.Reader) {
	isEquip := reader.ReadUint32() == 1
	deleteSlot := uint16(reader.ReadUint32())
	isInventory := reader.ReadUint32() == 1
	createSlot := uint16(reader.ReadUint32())

	var id int32

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	id, err = context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	switch {
	case isEquip && !isInventory:
		// from equipment to inventory
		ok, err := ctx.Char.Equipment.UnEquipItem(deleteSlot, createSlot, ctx.Char.Inventory)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}

		pkt := network.NewWriter(NFY_ITEM_UNEQUIP)
		pkt.WriteInt32(id)
		pkt.WriteInt16(deleteSlot)

		ctx.World.BroadcastSessionPacket(session, pkt)

		// with one-handed dual weapons we need to move from left hand to
		// the right, if right-hand weapon was removed
		// todo: check for dual-handed weapons and ignore
		if deleteSlot == inventory.RightHand {
			// switch weapon
			ctx.Char.Equipment.MoveItem(inventory.LeftHand, inventory.RightHand)
		}
	case isInventory && !isEquip:
		// from inventory to equipment
		ok, err := ctx.Char.Equipment.EquipItem(deleteSlot, createSlot, ctx.Char.Inventory)
		item := ctx.Char.Equipment.Get(createSlot)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}

		pkt := network.NewWriter(NFY_ITEM_EQUIP)
		pkt.WriteInt32(id)
		pkt.WriteInt32(item.Kind)
		pkt.WriteInt16(item.Slot)
		pkt.WriteInt32(0)
		pkt.WriteByte(0)

		ctx.World.BroadcastSessionPacket(session, pkt)
	case isEquip && isInventory:
		// exchanging equipment items? rings? because on weaps it doesn't work
		ok, err := ctx.Char.Equipment.MoveItem(deleteSlot, createSlot)
		item := ctx.Char.Equipment.Get(createSlot)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}

		pkt := network.NewWriter(NFY_ITEM_UNEQUIP)
		pkt.WriteInt32(id)
		pkt.WriteInt16(deleteSlot)
		ctx.World.BroadcastSessionPacket(session, pkt)

		pkt = network.NewWriter(NFY_ITEM_EQUIP)
		pkt.WriteInt32(id)
		pkt.WriteInt32(item.Kind)
		pkt.WriteInt16(item.Slot)
		pkt.WriteInt32(0)
		pkt.WriteByte(0)
		ctx.World.BroadcastSessionPacket(session, pkt)
	case !isEquip && !isInventory:
		// moving item in inventory
		ok, err := ctx.Char.Inventory.Move(deleteSlot, createSlot)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}
	default:
		notifyStorageExchange(session, false)
		return
	}
}

func StorageItemSwap(session *network.Session, reader *network.Reader) {
	src := StorageType(reader.ReadInt32())
	srcSlot := uint16(reader.ReadInt32())
	dst := StorageType(reader.ReadInt32())
	dstSlot := uint16(reader.ReadInt32())

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	state, err := false, nil

	inv := ctx.Char.Inventory
	eq := &ctx.Char.Equipment

	switch src {
	case Inventory:
		switch dst {
		case Inventory:
			state, err = inv.Swap(srcSlot, dstSlot)
		case Equipment:
			state, err = eq.SwapEquipItem(srcSlot, dstSlot, inv)
		}
	case Equipment:
		switch dst {
		case Inventory:
			/* do nothing */
			return
		case Equipment:
			state, err = eq.Swap(srcSlot, dstSlot)
		}
	}

	if err != nil {
		log.Error(err.Error())
	}

	pkt := network.NewWriter(STORAGE_ITEM_SWAP)
	pkt.WriteBool(state)

	session.Send(pkt)
}

func QueryCashItem(session *network.Session, reader *network.Reader) {
	pkt := network.NewWriter(QUERYCASHITEM)
	pkt.WriteInt32(0) //count

	//pkt.WriteInt32(1) //cash item id
	//pkt.WriteInt32(1) //kind
	//pkt.WriteInt32(0) // option
	//pkt.WriteByte(31) // duration

	session.Send(pkt)
}

func UseCashItem(session *network.Session, reader *network.Reader) {
	id := reader.ReadInt32()   // cash item id
	slot := reader.ReadInt16() // slot ??

	pkt := network.NewWriter(USECASHITEM)
	pkt.WriteInt32(id)   //count
	pkt.WriteInt32(1)    //item
	pkt.WriteInt32(0)    // option
	pkt.WriteInt16(slot) // slot
	pkt.WriteInt32(30)   //time
	pkt.WriteInt32(0)    // status

	session.Send(pkt)
}

func StorageItemDrop(session *network.Session, reader *network.Reader) {
	_ = reader.ReadInt32() // unk
	slot := reader.ReadUint16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	charId := ctx.Char.Id
	world := ctx.World
	x := int(ctx.Char.X)
	y := int(ctx.Char.Y)
	ctx.Mutex.RUnlock()

	item := ctx.Char.Inventory.Get(slot)

	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	state, err := ctx.Char.Inventory.Remove(slot)

	if err != nil {
		log.Error(err.Error())
	}

	pkt := network.NewWriter(STORAGE_ITEM_DROP)
	pkt.WriteBool(state)

	session.Send(pkt)

	if state {
		world.DropItem(&item, charId, x, y)
	}
}

func AccessoryEquip(session *network.Session, reader *network.Reader) {
	type AccessoryType int

	const (
		Earring AccessoryType = iota + 1
		Bracelet
		Ring
	)

	slot := uint16(reader.ReadUint32())
	reader.ReadInt32() // seem to be identical to slot
	accyType := AccessoryType(reader.ReadInt32())

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	// we need to un-equip last type
	// then switch 1st to 2nd, 2nd to 3rd, 3rd to 4th
	// and equip the one from inventory in the 1st type
	var slots []inventory.EquipmentType

	switch accyType {
	case Earring:
		slots = []inventory.EquipmentType{
			inventory.LeftEarring, inventory.RightEarring,
		}
	case Bracelet:
		slots = []inventory.EquipmentType{
			inventory.LeftBracelet, inventory.RightBracelet,
		}

	case Ring:
		slots = []inventory.EquipmentType{
			inventory.Ring1, inventory.Ring2, inventory.Ring3, inventory.Ring4,
		}
	default:
		log.Error("Unknown accessory type:", accyType)
		return
	}

	ctx.Mutex.Lock()
	ok, err := ctx.Char.Equipment.EquipAccessory(slot, slots, ctx.Char.Inventory)
	ctx.Mutex.Unlock()

	if err != nil {
		log.Error(err.Error())
	}

	pkt := network.NewWriter(ACCESSORY_EQUIP)
	pkt.WriteBool(ok)

	session.Send(pkt)
}

func ItemSelling(session *network.Session, reader *network.Reader) {
	pkt := network.NewWriter(STORAGE_ITEM_DROP)
	pkt.WriteInt64(10000)
	pkt.WriteInt32(0)
	pkt.WriteInt32(0)
	pkt.WriteInt16(0)

	session.Send(pkt)
}

func BuySkillBook(session *network.Session, reader *network.Reader) {
	npcIndex := reader.ReadByte()
	setIndex := reader.ReadUint16()
	skillIndex := reader.ReadUint16()
	slot := reader.ReadUint16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendBuySkillBookResult(session, false, inventory.Item{})
		return
	}

	ctx.Mutex.RLock()
	worldID := ctx.Char.World
	characterID := ctx.Char.Id
	characterInventory := ctx.Char.Inventory
	ctx.Mutex.RUnlock()

	book, exists := clientdata.FindSkillBook(worldID, npcIndex, setIndex)
	if !exists || book.SkillID != skillIndex {
		log.Warningf(
			"Rejected skill book purchase: character %d, world %d, NPC %d, set %d, skill %d",
			characterID, worldID, npcIndex, setIndex, skillIndex)
		sendBuySkillBookResult(session, false, inventory.Item{})
		return
	}

	item := inventory.Item{Kind: book.ItemID, Slot: slot}
	result, alz, err := characterInventory.Purchase(item, uint64(book.Price))
	if err != nil {
		log.Errorf("Unable to purchase skill book item %d for character %d: %s", book.ItemID, characterID, err.Error())
		sendBuySkillBookResult(session, false, inventory.Item{})
		return
	}
	if result {
		ctx.Mutex.Lock()
		ctx.Char.Alz = alz
		ctx.Mutex.Unlock()
		log.Infof(
			"Character %d purchased skill book item %d for skill %d at %d Alz in slot %d",
			characterID, book.ItemID, book.SkillID, book.Price, slot)
	}

	sendBuySkillBookResult(session, result, item)
}

func sendBuySkillBookResult(session *network.Session, result bool, item inventory.Item) {
	pkt := network.NewWriter(BUY_SKILL_BOOK)
	if result {
		pkt.WriteByte(0)
	} else {
		pkt.WriteByte(1)
	}
	pkt.WriteUint32(item.Kind)
	pkt.WriteUint32(item.Serials)
	pkt.WriteInt32(item.Option)
	pkt.WriteUint32(uint32(item.Slot))

	session.Send(pkt)
}

func ItemUsing(session *network.Session, reader *network.Reader) {
	slot := reader.ReadUint16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendItemUsingResult(session, false)
		return
	}

	ctx.Mutex.RLock()
	characterID := ctx.Char.Id
	characterInventory := ctx.Char.Inventory
	ctx.Mutex.RUnlock()

	item := characterInventory.Get(slot)
	book, isSkillBook := clientdata.FindSkillBookItem(item.Kind)
	if !isSkillBook {
		log.Warningf("Rejected unsupported item use: character %d, item %d, slot %d", characterID, item.Kind, slot)
		sendItemUsingResult(session, false)
		return
	}

	ctx.Mutex.RLock()
	for _, learned := range ctx.Char.Skills.List {
		if learned.Id == book.SkillID {
			ctx.Mutex.RUnlock()
			log.Warningf("Character %d already knows skill %d", characterID, book.SkillID)
			sendItemUsingResult(session, false)
			return
		}
	}

	skillSlot := -1
	for candidate := 0; candidate <= int(^uint16(0)); candidate++ {
		if _, occupied := ctx.Char.Skills.List[candidate]; !occupied {
			skillSlot = candidate
			break
		}
	}
	ctx.Mutex.RUnlock()
	if skillSlot < 0 {
		log.Warningf("Character %d has no free skill slot", characterID)
		sendItemUsingResult(session, false)
		return
	}

	skill := skills.Skill{Id: book.SkillID, Level: book.Level, Slot: uint16(skillSlot)}
	result, err := characterInventory.ConsumeSkillBook(slot, book.ItemID, skill)
	if err != nil {
		log.Errorf("Unable to use skill book item %d for character %d: %s", book.ItemID, characterID, err.Error())
		sendItemUsingResult(session, false)
		return
	}
	if result {
		ctx.Mutex.Lock()
		ctx.Char.Skills.Set(skill.Slot, skill)
		ctx.Mutex.Unlock()
		log.Infof(
			"Character %d learned skill %d level %d from item %d in skill slot %d",
			characterID, skill.Id, skill.Level, book.ItemID, skill.Slot)
	}

	sendItemUsingResult(session, result)
}

func sendItemUsingResult(session *network.Session, result bool) {
	pkt := network.NewWriter(ITEM_USING)
	if result {
		pkt.WriteByte(0)
	} else {
		pkt.WriteByte(1)
	}

	session.Send(pkt)
}
