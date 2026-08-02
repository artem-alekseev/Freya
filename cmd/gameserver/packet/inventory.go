package packet

import (
	"time"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/cashinventory"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

type StorageType int

const (
	Inventory StorageType = iota
	Equipment
	Warehouse
)

func notifyStorageExchange(session *network.Session, result bool) {
	packet := network.NewWriter(STORAGE_EXCHANGE_MOVE)
	packet.WriteBool(result)

	session.Send(packet)
}

func StorageExchangeMove(session *network.Session, reader *network.Reader) {
	sourceType := StorageType(reader.ReadInt32())
	sourceSlotValue := reader.ReadInt32()
	targetType := StorageType(reader.ReadInt32())
	targetSlotValue := reader.ReadInt32()

	if sourceSlotValue < 0 || sourceSlotValue > int32(^uint16(0)) ||
		targetSlotValue < 0 || targetSlotValue > int32(^uint16(0)) {
		notifyStorageExchange(session, false)
		return
	}

	deleteSlot := uint16(sourceSlotValue)
	createSlot := uint16(targetSlotValue)
	log.Debugf("StorageExchangeMove: source=%d slot=%d target=%d slot=%d",
		sourceType, deleteSlot, targetType, createSlot)

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
	case sourceType == Equipment && targetType == Inventory:
		// from equipment to inventory
		sourceSlot := deleteSlot
		if ctx.Char.Equipment.Get(sourceSlot).Kind == 0 {
			switch sourceSlot {
			case uint16(inventory.RightHand):
				if ctx.Char.Equipment.Get(uint16(inventory.LeftHand)).Kind != 0 {
					sourceSlot = uint16(inventory.LeftHand)
				}
			case uint16(inventory.LeftHand):
				if ctx.Char.Equipment.Get(uint16(inventory.RightHand)).Kind != 0 {
					sourceSlot = uint16(inventory.RightHand)
				}
			}
		}
		if sourceSlot != deleteSlot {
			log.Debugf("StorageExchangeMove: resolving empty equipment slot %d to %d",
				deleteSlot, sourceSlot)
		}

		ok, err := ctx.Char.Equipment.UnEquipItem(sourceSlot, createSlot, ctx.Char.Inventory)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}
		if !ok {
			return
		}

		pkt := network.NewWriter(NFY_ITEM_UNEQUIP)
		pkt.WriteInt32(id)
		pkt.WriteInt16(deleteSlot)

		ctx.World.BroadcastSessionPacket(session, pkt)

		// Keep the remaining weapon in its original slot. WorldSvr normalizes
		// primary weapon slots internally, but this client continues to send
		// the original slot in the next storage request and does not consume
		// that server-side relocation notification.
	case sourceType == Inventory && targetType == Equipment:
		// from inventory to equipment
		ok, err := ctx.Char.Equipment.EquipItem(deleteSlot, createSlot, ctx.Char.Inventory)
		item := ctx.Char.Equipment.Get(createSlot)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}
		if !ok {
			return
		}

		pkt := network.NewWriter(NFY_ITEM_EQUIP)
		pkt.WriteInt32(id)
		pkt.WriteUint32(item.Kind)
		pkt.WriteUint16(item.Slot)

		ctx.World.BroadcastSessionPacket(session, pkt)
	case sourceType == Equipment && targetType == Equipment:
		// exchanging equipment items? rings? because on weaps it doesn't work
		ok, err := ctx.Char.Equipment.MoveItem(deleteSlot, createSlot)
		item := ctx.Char.Equipment.Get(createSlot)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}
		if !ok {
			return
		}

		pkt := network.NewWriter(NFY_ITEM_UNEQUIP)
		pkt.WriteInt32(id)
		pkt.WriteInt16(deleteSlot)
		ctx.World.BroadcastSessionPacket(session, pkt)

		pkt = network.NewWriter(NFY_ITEM_EQUIP)
		pkt.WriteInt32(id)
		pkt.WriteUint32(item.Kind)
		pkt.WriteUint16(item.Slot)
		ctx.World.BroadcastSessionPacket(session, pkt)
	case sourceType == Inventory && targetType == Inventory:
		// moving item in inventory
		ok, err := ctx.Char.Inventory.Move(deleteSlot, createSlot)

		notifyStorageExchange(session, ok)

		if err != nil {
			log.Error(err.Error())
			return
		}
	case (sourceType == Inventory || sourceType == Warehouse) &&
		(targetType == Inventory || targetType == Warehouse):
		ok, err := moveStorageItem(ctx, id, sourceType, deleteSlot, targetType, createSlot)
		if err != nil {
			log.Error(err.Error())
		}
		notifyStorageExchange(session, ok)
	default:
		notifyStorageExchange(session, false)
		return
	}
}

func moveStorageItem(ctx *context.Context, characterID int32, sourceType StorageType, sourceSlot uint16,
	targetType StorageType, targetSlot uint16) (bool, error) {
	var source *inventory.Inventory
	if sourceType == Inventory {
		source = ctx.Char.Inventory
	} else {
		source = &ctx.Warehouse
	}

	var target *inventory.Inventory
	if targetType == Inventory {
		target = ctx.Char.Inventory
	} else {
		target = &ctx.Warehouse
	}

	item := source.Get(sourceSlot)
	if item.Kind == 0 {
		return false, nil
	}
	if sourceType != targetType || sourceSlot != targetSlot {
		if target.Get(targetSlot).Kind != 0 {
			return false, nil
		}
	}

	request := inventory.StorageMoveRequest{
		Server:     byte(g_ServerSettings.ServerId),
		Character:  characterID,
		SourceType: byte(sourceType),
		SourceSlot: sourceSlot,
		TargetType: byte(targetType),
		TargetSlot: targetSlot,
	}
	response := inventory.StorageMoveResponse{}
	if err := g_RPCHandler.Call(rpc.StorageMove, &request, &response); err != nil {
		return false, err
	}
	if !response.Result {
		return false, nil
	}

	if sourceType != targetType || sourceSlot != targetSlot {
		source.RemoveLocal(sourceSlot)
		item.Slot = targetSlot
		target.SetLocal(item)
	}

	return true, nil
}

func StorageItemSwap(session *network.Session, reader *network.Reader) {
	src, srcSlotValue := readStorageSlot(reader)
	dst, dstSlotValue := readStorageSlot(reader)
	src2, src2SlotValue := readStorageSlot(reader)
	dst2, dst2SlotValue := readStorageSlot(reader)

	// AdditionInfo: WarehouseCheckType (byte + padding + int).
	_ = reader.ReadByte()
	_ = reader.ReadBytes(3)
	_ = reader.ReadInt32()

	if !validStorageSlot(src, srcSlotValue) || !validStorageSlot(dst, dstSlotValue) ||
		!validStorageSlot(src2, src2SlotValue) || !validStorageSlot(dst2, dst2SlotValue) ||
		src != dst2 || src2 != dst {
		sendStorageSwapResult(session, false)
		return
	}

	srcSlot := uint16(srcSlotValue)
	dstSlot := uint16(dstSlotValue)

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
			// The client may send the pair in the opposite direction. Reuse
			// the inventory -> equipment transaction with reversed slots.
			state, err = eq.SwapEquipItem(dstSlot, srcSlot, inv)
		case Equipment:
			state, err = eq.Swap(srcSlot, dstSlot)
		}
	}

	if err != nil {
		log.Error(err.Error())
	}

	sendStorageSwapResult(session, state)
}

func readStorageSlot(reader *network.Reader) (StorageType, int32) {
	return StorageType(reader.ReadInt32()), reader.ReadInt32()
}

func validStorageSlot(storageType StorageType, slot int32) bool {
	return storageType >= Inventory && storageType <= Warehouse &&
		slot >= 0 && slot <= int32(^uint16(0))
}

func sendStorageSwapResult(session *network.Session, result bool) {
	pkt := network.NewWriter(STORAGE_ITEM_SWAP)
	pkt.WriteBool(result)
	session.Send(pkt)
}

func QueryCashItem(session *network.Session, reader *network.Reader) {
	_ = reader
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	characterID := ctx.Char.Id
	ctx.Mutex.RUnlock()
	request := cashinventory.LoadRequest{
		Server:    byte(g_ServerSettings.ServerId),
		Character: characterID,
	}
	response := cashinventory.LoadResponse{}
	if err := g_RPCHandler.Call(rpc.LoadCashInventory, &request, &response); err != nil {
		log.Errorf("Unable to load cash inventory for character %d: %s", characterID, err)
	} else {
		ctx.CashInventory.Init(response.Items)
	}
	items := ctx.CashInventory.List()
	pkt := network.NewWriter(QUERYCASHITEM)
	pkt.WriteInt32(len(items))
	for _, item := range items {
		pkt.WriteInt32(item.ID)
		pkt.WriteUint32(item.Kind)
		pkt.WriteInt32(item.Option)
		pkt.WriteByte(item.DurationID)
	}

	session.Send(pkt)
}

func UseCashItem(session *network.Session, reader *network.Reader) {
	id := reader.ReadInt32()
	slot := reader.ReadUint16()
	result := cashinventory.UseDatabaseFailed
	item := inventory.Item{}

	ctx, err := context.Parse(session)
	if err == nil {
		cashItem, exists := ctx.CashInventory.Get(id)
		switch {
		case !exists:
			result = cashinventory.UseNotFound
		case slot >= cashInventorySlotCount:
			result = cashinventory.UseSlotOccupied
		case ctx.Char.Inventory.Get(slot).Kind != 0:
			result = cashinventory.UseSlotOccupied
		default:
			expire, valid := clientdata.CashItemExpiration(cashItem.DurationID)
			if !valid {
				result = cashinventory.UseCreateFailed
				break
			}

			req := cashinventory.UseRequest{
				Server:    byte(g_ServerSettings.ServerId),
				Character: ctx.Char.Id,
				CashID:    cashItem.ID,
				Slot:      slot,
				Item:      cashItem,
				Expire:    expire,
			}
			res := cashinventory.UseResponse{}
			if rpcErr := g_RPCHandler.Call(rpc.UseCashItem, &req, &res); rpcErr != nil {
				log.Errorf("Unable to use cash item %d for character %d: %s", id, ctx.Char.Id, rpcErr)
				break
			}
			result = res.Result
			if result == cashinventory.UseOK {
				item = res.Item
				ctx.Char.Inventory.SetLocal(item)
				ctx.CashInventory.Remove(id)
			}
		}
	} else {
		log.Error(err.Error())
	}

	pkt := network.NewWriter(USECASHITEM)
	pkt.WriteInt32(id)
	pkt.WriteUint32(item.Kind)
	pkt.WriteInt32(item.Option)
	pkt.WriteUint16(item.Slot)
	pkt.WriteUint32(item.Expire)
	pkt.WriteInt32(result)
	session.Send(pkt)
}

const (
	// WorldSvr defines INVENTORY_SLOTNUM as 256 normal slots plus 10
	// temporary slots. Cash items are accepted anywhere in that range.
	normalInventorySlotCount uint16 = 256
	tempInventorySlotCount   uint16 = 10
	cashInventorySlotCount          = normalInventorySlotCount + tempInventorySlotCount
)

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
	const (
		itemSellingSuccess         int32 = 0
		itemSellingFailByOperation int32 = 1
		itemSellingFailByIndex     int32 = 2
		itemSellingFailByNoSell    int32 = 4
		itemSellingFailByAlzStatus int32 = 5
		itemSellingFailBySoldOver  int32 = 6
		maxItemSellingCount              = 128
	)

	npcIndex := reader.ReadByte()
	remoteShopSlot := reader.ReadInt32()
	tradeCount := reader.ReadInt32()
	if remoteShopSlot < 0 || remoteShopSlot > int32(^uint16(0)) {
		sendItemSellingResult(session, 0, itemSellingFailByIndex)
		return
	}
	if tradeCount <= 0 || tradeCount > maxItemSellingCount {
		sendItemSellingResult(session, 0, itemSellingFailByOperation)
		return
	}

	slots := make([]uint16, tradeCount)
	seenSlots := make(map[uint16]struct{}, tradeCount)
	for i := range slots {
		slot := reader.ReadInt32()
		if slot < 0 || slot > int32(^uint16(0)) {
			sendItemSellingResult(session, 0, itemSellingFailByOperation)
			return
		}

		slots[i] = uint16(slot)
		if _, exists := seenSlots[slots[i]]; exists {
			sendItemSellingResult(session, 0, itemSellingFailByOperation)
			return
		}
		seenSlots[slots[i]] = struct{}{}
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Errorf("[ITEMSELLING] %s", err)
		sendItemSellingResult(session, 0, itemSellingFailByOperation)
		return
	}

	ctx.Mutex.RLock()
	if ctx.Char == nil || ctx.Char.Inventory == nil {
		ctx.Mutex.RUnlock()
		sendItemSellingResult(session, 0, itemSellingFailByOperation)
		return
	}
	characterID := ctx.Char.Id
	characterAlz := ctx.Char.Alz
	characterInventory := ctx.Char.Inventory
	ctx.Mutex.RUnlock()

	items := make([]inventory.Item, len(slots))
	var sellPrice uint64
	for i, slot := range slots {
		item := characterInventory.Get(slot)
		if item.Kind == 0 {
			sendItemSellingResult(session, 0, itemSellingFailByOperation)
			return
		}

		price, exists := clientdata.FindItemSellValue(item.Kind, item.Option)
		if !exists || price == 0 {
			log.Warningf("[ITEMSELLING] Item %d in slot %d cannot be sold", item.Kind, slot)
			sendItemSellingResult(session, 0, itemSellingFailByNoSell)
			return
		}
		if ^uint64(0)-sellPrice < price {
			sendItemSellingResult(session, 0, itemSellingFailBySoldOver)
			return
		}

		items[i] = item
		sellPrice += price
	}
	if ^uint64(0)-characterAlz < sellPrice {
		sendItemSellingResult(session, characterAlz, itemSellingFailByAlzStatus)
		return
	}

	sold, alz, err := characterInventory.Sell(items, sellPrice)
	if err != nil {
		log.Errorf("[ITEMSELLING] Unable to sell items for character %d: %s", characterID, err)
		sendItemSellingResult(session, 0, itemSellingFailByOperation)
		return
	}
	if !sold {
		sendItemSellingResult(session, alz, itemSellingFailByOperation)
		return
	}

	ctx.Mutex.Lock()
	ctx.Char.Alz = alz
	ctx.Mutex.Unlock()

	log.Infof("[ITEMSELLING] Character %d sold %d item(s) to NPC %d for %d Alz",
		characterID, len(items), npcIndex, sellPrice)
	sendItemSellingResult(session, alz, itemSellingSuccess)
}

func sendItemSellingResult(session *network.Session, alz uint64, result int32) {
	pkt := network.NewWriter(ITEMSELLING)
	pkt.WriteUint64(alz)
	pkt.WriteInt32(result)
	session.Send(pkt)
}

func ItemBuyings(session *network.Session, reader *network.Reader) {
	const (
		itemBuyingSuccess         int32 = 0
		itemBuyingFailByIndex     int32 = 1
		itemBuyingFailByOperation int32 = 2
		itemBuyingFailByAlz       int32 = 5
		itemBuyingHeaderSize            = 10
		itemBuyingNonGroupSize          = itemBuyingHeaderSize + 1 + 4 + 4 + 4 + 2
	)

	npcIndex := reader.ReadByte()
	setIndex := reader.ReadInt32()
	remoteShopSlot := reader.ReadInt32()
	var requestedKind uint32
	var tradeCount int32
	var inventorySlot int32
	if int(reader.Size) == itemBuyingNonGroupSize {
		// Non-group clients include the complete kind index in the request.
		requestedKind = reader.ReadUint32()
		inventorySlot = int32(reader.ReadUint16())
		tradeCount = 1
	} else {
		// Group-trade clients send a count followed by free inventory slots;
		// the kind is taken from the validated shop entry below.
		tradeCount = reader.ReadInt32()
		inventorySlot = reader.ReadInt32()
	}

	result := itemBuyingFailByIndex
	item := inventory.Item{}
	if remoteShopSlot < 0 || tradeCount != 1 || inventorySlot < 0 || inventorySlot > int32(^uint16(0)) {
		sendItemBuyingsResult(session, result, item)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Errorf("[ITEMBUYINGS] %s", err)
		sendItemBuyingsResult(session, itemBuyingFailByOperation, item)
		return
	}

	ctx.Mutex.RLock()
	if ctx.Char == nil || ctx.Char.Inventory == nil {
		ctx.Mutex.RUnlock()
		sendItemBuyingsResult(session, itemBuyingFailByOperation, item)
		return
	}
	worldID := ctx.Char.World
	characterID := ctx.Char.Id
	characterAlz := ctx.Char.Alz
	characterInventory := ctx.Char.Inventory
	ctx.Mutex.RUnlock()

	shopItem, exists := clientdata.FindShopItem(worldID, npcIndex, setIndex)
	shopKind := shopItem.Kind
	if shopKind == 0 {
		shopKind = shopItem.ItemID
	}
	if !exists || shopKind == 0 || shopItem.Price == 0 {
		log.Warningf("[ITEMBUYINGS] Unknown shop item: character %d, world %d, NPC %d, slot %d",
			characterID, worldID, npcIndex, setIndex)
		sendItemBuyingsResult(session, result, item)
		return
	}
	if requestedKind != 0 && requestedKind != shopKind {
		log.Warningf("[ITEMBUYINGS] Kind mismatch: character %d, world %d, NPC %d, slot %d, client kind %d, shop kind %d",
			characterID, worldID, npcIndex, setIndex, requestedKind, shopKind)
		sendItemBuyingsResult(session, result, item)
		return
	}

	item = inventory.Item{
		Kind:   shopKind,
		Option: shopItem.Option,
		Slot:   uint16(inventorySlot),
	}

	purchased, alz, err := characterInventory.Purchase(item, shopItem.Price)
	if err != nil {
		log.Errorf("[ITEMBUYINGS] Unable to buy item %d for character %d: %s", item.Kind, characterID, err)
		sendItemBuyingsResult(session, itemBuyingFailByOperation, inventory.Item{})
		return
	}
	if !purchased {
		if alz < shopItem.Price || characterAlz < shopItem.Price {
			result = itemBuyingFailByAlz
		} else {
			result = itemBuyingFailByOperation
		}
		sendItemBuyingsResult(session, result, inventory.Item{})
		return
	}

	ctx.Mutex.Lock()
	ctx.Char.Alz = alz
	ctx.Mutex.Unlock()

	log.Infof("[ITEMBUYINGS] Character %d bought item %d from NPC %d for %d Alz in slot %d",
		characterID, item.Kind, npcIndex, shopItem.Price, item.Slot)
	sendItemBuyingsResult(session, itemBuyingSuccess, item)
}

func sendItemBuyingsResult(session *network.Session, result int32, item inventory.Item) {
	pkt := network.NewWriter(ITEMBUYINGS)
	pkt.WriteInt32(result)
	pkt.WriteUint32(item.Kind)
	pkt.WriteInt32(item.Option)
	pkt.WriteUint16(item.Slot)
	pkt.WriteUint32(item.Expire)
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
	if recovery, isManaPotion := clientdata.FindManaPotionRecovery(item.Kind); isManaPotion {
		useManaPotion(session, ctx, slot, item, recovery)
		return
	}

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

	slotStart, slotEnd := uint16(0), uint16(^uint8(0))
	if start, end, ok := clientdata.SkillSlotRange(book.SkillID); ok {
		slotStart, slotEnd = start, end
	} else {
		log.Warningf("Skill %d has no physical/magic slot range, using the common skill slot range", book.SkillID)
	}

	skillSlot := -1
	for candidate := slotStart; candidate <= slotEnd; candidate++ {
		if _, occupied := ctx.Char.Skills.List[int(candidate)]; !occupied {
			skillSlot = int(candidate)
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

const manaPotionCooldown = 2 * time.Second

func useManaPotion(session *network.Session, ctx *context.Context, slot uint16, item inventory.Item, recovery uint16) {
	now := time.Now()
	ctx.Mutex.Lock()
	if ctx.Char == nil || ctx.Char.CurrentMP >= ctx.Char.MaxMP ||
		item.Option <= 0 || now.Before(ctx.MPPotionCooldownUntil) {
		characterID := int32(0)
		if ctx.Char != nil {
			characterID = ctx.Char.Id
		}
		ctx.Mutex.Unlock()
		log.Warningf("Rejected mana potion use: character %d, item %d, slot %d", characterID, item.Kind, slot)
		sendItemUsingResult(session, false)
		return
	}
	characterID := ctx.Char.Id
	ctx.MPPotionCooldownUntil = now.Add(manaPotionCooldown)
	ctx.Mutex.Unlock()

	updatedItem, consumed, err := ctx.Char.Inventory.ConsumeOne(slot)
	if err != nil || !consumed {
		ctx.Mutex.Lock()
		ctx.MPPotionCooldownUntil = time.Time{}
		ctx.Mutex.Unlock()
		if err != nil {
			log.Errorf("Unable to consume mana potion %d for character %d: %s", item.Kind, characterID, err)
		}
		sendItemUsingResult(session, false)
		return
	}

	ctx.Mutex.Lock()
	currentMP := uint32(ctx.Char.CurrentMP) + uint32(recovery)
	if currentMP > uint32(ctx.Char.MaxMP) {
		currentMP = uint32(ctx.Char.MaxMP)
	}
	ctx.Char.CurrentMP = uint16(currentMP)
	newMP := ctx.Char.CurrentMP
	snapshot := *ctx.Char
	ctx.Mutex.Unlock()

	if !saveCharacterVitals(&snapshot) {
		log.Warningf("Mana potion was consumed but vitals were not saved: character %d", characterID)
	}

	// WorldSvr sends the potion-specific update first and the ITEMUSING
	// result second. The client uses this sequence to refresh the stack count.
	sendManaPotionUpdate(session, newMP)
	sendItemUsingResult(session, true)
	log.Infof("Character %d used mana potion %d in slot %d, restored %d MP, remaining %d",
		characterID, item.Kind, slot, recovery, updatedItem.Option)
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
