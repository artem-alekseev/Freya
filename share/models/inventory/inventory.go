package inventory

import (
	"bytes"
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/rpc"
)

type Inventory struct {
	Inv   map[int]Item
	mutex sync.RWMutex

	rpcHandler *rpc.Client
	character  int32
	serverId   byte
}

// Initializes Inventory
func (e *Inventory) Init() {
	e.Inv = make(map[int]Item)
}

func (e *Inventory) sync(cmd string, old *Item, new *Item) (bool, error) {
	// being initialized
	if e.character == 0 && e.serverId == 0 {
		return true, nil
	}

	if e.rpcHandler == nil {
		return false, errors.New("rpc handler is not ready")
	}

	req := ItemRequest{
		Server:  e.serverId,
		Id:      e.character,
		Command: cmd,
		Item:    *old,
		NewItem: new,
	}

	res := ItemResponse{}
	if err := e.rpcHandler.Call(cmd, &req, &res); err != nil {
		return false, err
	}

	return res.Result, nil
}

func (e *Inventory) Setup(rpc *rpc.Client, id int32, server byte) {
	e.rpcHandler = rpc
	e.character = id
	e.serverId = server
}

// Sets inventory item by slot
func (e *Inventory) Set(slot uint16, item Item) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	ok, err := e.sync(rpc.AddItem, &item, nil)
	if err == nil && ok {
		e.Inv[int(slot)] = item
	}

	return ok, err
}

// Purchase atomically persists a purchased item and its price before adding
// it to the in-memory inventory.
func (e *Inventory) Purchase(item Item, price uint64) (bool, uint64, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if _, exists := e.Inv[int(item.Slot)]; exists {
		return false, 0, errors.New("inventory slot is already occupied")
	}
	if e.rpcHandler == nil {
		return false, 0, errors.New("rpc handler is not ready")
	}

	req := PurchaseRequest{
		Server:    e.serverId,
		Character: e.character,
		Item:      item,
		Price:     price,
	}
	res := PurchaseResponse{}
	if err := e.rpcHandler.Call(rpc.PurchaseItem, &req, &res); err != nil {
		return false, 0, err
	}
	if !res.Result {
		return false, res.Alz, nil
	}

	e.Inv[int(item.Slot)] = item
	return true, res.Alz, nil
}

// Sell atomically removes the selected items and credits their price before
// updating the in-memory inventory.
func (e *Inventory) Sell(items []Item, price uint64) (bool, uint64, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if len(items) == 0 || price == 0 {
		return false, 0, nil
	}
	if e.rpcHandler == nil {
		return false, 0, errors.New("rpc handler is not ready")
	}

	seenSlots := make(map[uint16]struct{}, len(items))
	for _, item := range items {
		stored, exists := e.Inv[int(item.Slot)]
		if !exists || stored != item {
			return false, 0, errors.New("inventory item changed before selling")
		}
		if _, exists := seenSlots[item.Slot]; exists {
			return false, 0, errors.New("inventory slot is listed more than once")
		}
		seenSlots[item.Slot] = struct{}{}
	}

	req := SellRequest{
		Server:    e.serverId,
		Character: e.character,
		Items:     items,
		Price:     price,
	}
	res := SellResponse{}
	if err := e.rpcHandler.Call(rpc.SellItems, &req, &res); err != nil {
		return false, 0, err
	}
	if !res.Result {
		return false, res.Alz, nil
	}

	for _, item := range items {
		delete(e.Inv, int(item.Slot))
	}
	return true, res.Alz, nil
}

// ConsumeSkillBook atomically removes a skill book from persistent inventory
// and inserts the learned skill before updating the in-memory inventory.
func (e *Inventory) ConsumeSkillBook(slot uint16, itemID uint32, skill skills.Skill) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	item, exists := e.Inv[int(slot)]
	if !exists || item.Kind != itemID {
		return false, errors.New("skill book does not exist in the inventory slot")
	}
	if e.rpcHandler == nil {
		return false, errors.New("rpc handler is not ready")
	}

	req := skills.LearnRequest{
		Server:        e.serverId,
		Character:     e.character,
		InventorySlot: slot,
		ItemID:        itemID,
		Skill:         skill,
	}
	res := skills.SkillResponse{}
	if err := e.rpcHandler.Call(rpc.LearnSkill, &req, &res); err != nil {
		return false, err
	}
	if !res.Result {
		return false, nil
	}

	delete(e.Inv, int(slot))
	return true, nil
}

// ConsumeOne atomically removes one unit from a stackable inventory item.
// The item is deleted when its remaining option reaches zero.
func (e *Inventory) ConsumeOne(slot uint16) (Item, bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	item, exists := e.Inv[int(slot)]
	if !exists || item.Kind == 0 || item.Option <= 0 {
		return Item{}, false, errors.New("stackable item does not exist in the inventory slot")
	}
	if e.rpcHandler == nil {
		return Item{}, false, errors.New("rpc handler is not ready")
	}

	updated := item
	updated.Option--
	ok, err := e.sync(rpc.ConsumeItem, &item, &updated)
	if err != nil || !ok {
		return Item{}, false, err
	}

	if updated.Option == 0 {
		delete(e.Inv, int(slot))
	} else {
		e.Inv[int(slot)] = updated
	}

	return updated, true, nil
}

// Stack inventory item
func (e *Inventory) Stack(slot uint16, total int32) (bool, error) {
	// update amount
	item := e.Get(slot)
	item.Option = total

	e.mutex.Lock()
	defer e.mutex.Unlock()

	ok, err := e.sync(rpc.StackItem, &item, nil)
	if err == nil {
		delete(e.Inv, int(slot))
		e.Inv[int(slot)] = item
	}

	return ok, err
}

// Returns inventory item by slot
func (e *Inventory) Get(slot uint16) Item {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	if value, ok := e.Inv[int(slot)]; ok {
		return value
	}

	return Item{}
}

// SetLocal updates an item after a transaction has already been persisted by
// the Master Server. It intentionally skips the regular inventory RPC.
func (e *Inventory) SetLocal(item Item) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if e.Inv == nil {
		e.Inv = make(map[int]Item)
	}
	e.Inv[int(item.Slot)] = item
}

// RemoveLocal removes an item after a transaction has already been persisted
// by the Master Server. It intentionally skips the regular inventory RPC.
func (e *Inventory) RemoveLocal(slot uint16) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	delete(e.Inv, int(slot))
}

// Removes inventory item by slot
func (e *Inventory) Remove(slot uint16) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	item, ok := e.Inv[int(slot)]
	if !ok {
		return ok, errors.New("such item does not exist in the inventory")
	}

	ok, err := e.sync(rpc.RemoveItem, &item, nil)
	if err == nil {
		delete(e.Inv, int(slot))
	}

	return ok, err
}

// Enchant atomically persists the new target kind and removes the consumed
// core items before updating the in-memory inventory.
func (e *Inventory) Enchant(targetSlot uint16, newKind uint32, coreSlots []uint16) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	target, exists := e.Inv[int(targetSlot)]
	if !exists || target.Kind == 0 {
		return false, errors.New("target item does not exist in the inventory")
	}
	if newKind == 0 || len(coreSlots) == 0 {
		return false, errors.New("invalid enchant request")
	}
	if e.rpcHandler == nil {
		return false, errors.New("rpc handler is not ready")
	}

	cores := make([]Item, 0, len(coreSlots))
	seen := make(map[uint16]struct{}, len(coreSlots))
	for _, slot := range coreSlots {
		if slot == targetSlot {
			return false, errors.New("target item cannot also be a core")
		}
		if _, exists := seen[slot]; exists {
			return false, errors.New("enchant core slot is listed more than once")
		}
		seen[slot] = struct{}{}

		core, exists := e.Inv[int(slot)]
		if !exists || core.Kind == 0 {
			return false, errors.New("enchant core does not exist in the inventory")
		}
		cores = append(cores, core)
	}

	req := EnchantRequest{
		Server:    e.serverId,
		Character: e.character,
		Target:    target,
		NewKind:   newKind,
		Cores:     cores,
	}
	res := EnchantResponse{}
	if err := e.rpcHandler.Call(rpc.EnchantItem, &req, &res); err != nil {
		return false, err
	}
	if !res.Result {
		return false, nil
	}

	target.Kind = newKind
	e.Inv[int(targetSlot)] = target
	for _, core := range cores {
		delete(e.Inv, int(core.Slot))
	}

	return true, nil
}

// ForceCoreEnchant atomically updates a target item's option, decrements the
// submitted scrolls and removes the submitted force-core items.
func (e *Inventory) ForceCoreEnchant(target Item, newTarget Item, randomScroll *Item, specificScroll *Item, cores []Item) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	storedTarget, exists := e.Inv[int(target.Slot)]
	if !exists || storedTarget != target || newTarget.Kind == 0 ||
		newTarget.Slot != target.Slot || newTarget.Serials != target.Serials ||
		newTarget.Expire != target.Expire || len(cores) == 0 {
		return false, errors.New("invalid force-core enchant target")
	}
	if e.rpcHandler == nil {
		return false, errors.New("rpc handler is not ready")
	}

	request := ForceCoreEnchantRequest{
		Server:    e.serverId,
		Character: e.character,
		Target:    target,
		NewTarget: newTarget,
		Cores:     append([]Item(nil), cores...),
	}
	if randomScroll != nil {
		copy := *randomScroll
		request.RandomScroll = &copy
	}
	if specificScroll != nil {
		copy := *specificScroll
		request.SpecificScroll = &copy
	}

	seen := map[uint16]struct{}{target.Slot: {}}
	materials := make([]Item, 0, len(cores)+2)
	coreSlots := make(map[uint16]struct{}, len(cores))
	for _, core := range cores {
		coreSlots[core.Slot] = struct{}{}
	}
	if randomScroll != nil {
		materials = append(materials, *randomScroll)
	}
	if specificScroll != nil {
		materials = append(materials, *specificScroll)
	}
	materials = append(materials, cores...)
	for _, material := range materials {
		if material.Kind == 0 {
			return false, errors.New("force-core material does not exist")
		}
		if _, exists := seen[material.Slot]; exists {
			return false, errors.New("force-core material slot is duplicated")
		}
		if stored, exists := e.Inv[int(material.Slot)]; !exists || stored != material {
			return false, errors.New("force-core material changed before enchanting")
		}
		seen[material.Slot] = struct{}{}
	}

	response := ForceCoreEnchantResponse{}
	if err := e.rpcHandler.Call(rpc.ForceCoreEnchant, &request, &response); err != nil {
		return false, err
	}
	if !response.Result {
		return false, nil
	}

	e.Inv[int(target.Slot)] = newTarget
	for _, material := range materials {
		if _, isCore := coreSlots[material.Slot]; isCore {
			delete(e.Inv, int(material.Slot))
			continue
		}
		if material.Option <= 0 {
			delete(e.Inv, int(material.Slot))
			continue
		}
		material.Option--
		if material.Option == 0 {
			delete(e.Inv, int(material.Slot))
		} else {
			e.Inv[int(material.Slot)] = material
		}
	}

	return true, nil
}

// Attach atomically transforms the target item and consumes the source item.
func (e *Inventory) Attach(sourceSlot, targetSlot uint16, newKind uint32) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	target, exists := e.Inv[int(targetSlot)]
	if !exists {
		return false, errors.New("target item does not exist in the inventory")
	}
	target.Kind = newKind
	return e.attachLocked(sourceSlot, targetSlot, target)
}

// AttachWithTarget atomically applies all changed fields of the target item
// and consumes the source item. It is used by attach operations that modify
// item options in addition to the item kind.
func (e *Inventory) AttachWithTarget(sourceSlot, targetSlot uint16, target Item) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	return e.attachLocked(sourceSlot, targetSlot, target)
}

func (e *Inventory) attachLocked(sourceSlot, targetSlot uint16, newTarget Item) (bool, error) {
	if sourceSlot == targetSlot {
		return false, errors.New("source and target inventory slots must differ")
	}
	if newTarget.Kind == 0 || newTarget.Slot != targetSlot {
		return false, errors.New("invalid attached target item")
	}

	source, sourceExists := e.Inv[int(sourceSlot)]
	target, targetExists := e.Inv[int(targetSlot)]
	if !sourceExists || source.Kind == 0 {
		return false, errors.New("source item does not exist in the inventory")
	}
	if !targetExists || target.Kind == 0 {
		return false, errors.New("target item does not exist in the inventory")
	}
	if newTarget.Serials != target.Serials || newTarget.Expire != target.Expire {
		return false, errors.New("attached target identity cannot be changed")
	}
	if e.rpcHandler == nil {
		return false, errors.New("rpc handler is not ready")
	}

	req := AttachRequest{
		Server:    e.serverId,
		Character: e.character,
		Source:    source,
		Target:    target,
		NewKind:   newTarget.Kind,
		NewTarget: &newTarget,
	}
	res := AttachResponse{}
	if err := e.rpcHandler.Call(rpc.AttachItem, &req, &res); err != nil {
		return false, err
	}
	if !res.Result {
		return false, nil
	}

	delete(e.Inv, int(sourceSlot))
	e.Inv[int(targetSlot)] = newTarget
	return true, nil
}

// Removes inventory item by slot
func (e *Inventory) Swap(old, new uint16) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	oldItem, ok := e.Inv[int(old)]
	if !ok {
		return ok, errors.New("such item does not exist in the inventory")
	}

	newItem, ok := e.Inv[int(new)]
	if !ok {
		return ok, errors.New("such item does not exist in the inventory")
	}

	// swap slots
	oldItem.Slot = new
	newItem.Slot = old

	ok, err := e.sync(rpc.SwapItem, &oldItem, &newItem)
	if err == nil {
		delete(e.Inv, int(old))
		delete(e.Inv, int(new))
		e.Inv[int(oldItem.Slot)] = oldItem
		e.Inv[int(newItem.Slot)] = newItem
	}

	return ok, err
}

// Move item to a new slot in inventory
func (e *Inventory) Move(old, new uint16) (bool, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	oldItem, ok := e.Inv[int(old)]
	if !ok {
		return ok, errors.New("such item does not exist in the inventory")
	}

	if _, ok = e.Inv[int(new)]; ok {
		return ok, errors.New("such item already exists in the inventory")
	}

	// make a copy and swap slot
	newItem := oldItem
	newItem.Slot = new

	ok, err := e.sync(rpc.MoveItem, &oldItem, &newItem)
	if err == nil {
		delete(e.Inv, int(old))
		e.Inv[int(newItem.Slot)] = newItem
	}

	return ok, err
}

// Serializes inventory into byte array
func (e *Inventory) Serialize() ([]byte, int) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	// collect keys for sorted iteration
	var keys []int
	for k := range e.Inv {
		keys = append(keys, k)
	}

	sort.Ints(keys)
	var length = 0

	var equip bytes.Buffer
	for _, value := range keys {
		if e.Inv[value].Kind > 0 {
			binary.Write(&equip, binary.LittleEndian, e.Inv[value])
			length++
		}
	}

	return equip.Bytes(), length
}
