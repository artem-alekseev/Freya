package cashinventory

import (
	"sort"
	"sync"

	"github.com/ubis/Freya/share/models/inventory"
)

// Item is a cash item waiting to be received by the character.
// durationIdx is the client/server duration table index, not an expiration
// timestamp. The timestamp is created when the item is received.
type Item struct {
	ID         int32  `db:"cash_id"`
	Kind       uint32 `db:"kind"`
	Option     int32  `db:"opt"`
	DurationID byte   `db:"duration_idx"`
}

// Inventory stores the character's pending cash items in memory while the
// character is logged in. The source of truth remains the World database.
type Inventory struct {
	items map[int32]Item
	mutex sync.RWMutex
}

func (i *Inventory) Init(items []Item) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	i.items = make(map[int32]Item, len(items))
	for _, item := range items {
		if item.ID > 0 && item.Kind != 0 {
			i.items[item.ID] = item
		}
	}
}

// List returns items in cash-inventory order by cash item id.
func (i *Inventory) List() []Item {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	result := make([]Item, 0, len(i.items))
	for _, item := range i.items {
		result = append(result, item)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ID < result[right].ID
	})
	return result
}

func (i *Inventory) Get(id int32) (Item, bool) {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	item, ok := i.items[id]
	return item, ok
}

func (i *Inventory) Remove(id int32) bool {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	if _, ok := i.items[id]; !ok {
		return false
	}
	delete(i.items, id)
	return true
}

func (i *Inventory) Add(item Item) {
	if item.ID <= 0 || item.Kind == 0 {
		return
	}

	i.mutex.Lock()
	defer i.mutex.Unlock()

	if i.items == nil {
		i.items = make(map[int32]Item)
	}
	i.items[item.ID] = item
}

// UseResponse contains the result of atomically receiving a cash item into
// the normal inventory.
type UseResponse struct {
	Result int32
	Item   inventory.Item
}

const (
	UseOK             int32 = 0
	UseNotFound       int32 = 1
	UseSlotOccupied   int32 = 2
	UseCreateFailed   int32 = 8
	UseDatabaseFailed int32 = 9
)
