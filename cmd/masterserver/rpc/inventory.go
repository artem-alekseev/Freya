package rpc

import (
	"database/sql"
	"errors"

	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/rpc"
)

func EquipItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	_, err := db.MustExec("INSERT INTO characters_equipment "+
		"(id, kind, serials, opt, slot, expire) "+
		"VALUES (?, ?, ?, ?, ?, ?)",
		r.Id, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Slot, r.Item.Expire).RowsAffected()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

func UnEquipItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	result, err := db.MustExec(
		"DELETE FROM characters_equipment WHERE id = ? AND slot = ?",
		r.Id, r.Item.Slot).RowsAffected()
	if err != nil {
		return err
	}
	if result == 0 {
		return errors.New("equipment item was not found")
	}

	s.Result = true

	return nil
}

func SwapEquipmentItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	if r.NewItem == nil {
		return errors.New("target item is not set")
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	// the rollback will be ignored if the tx has been committed later in the function
	defer tx.Rollback()

	// slots were swapped before
	newSlot := r.Item.Slot
	oldSlot := r.NewItem.Slot

	// since id + slot is primary key, we need to switch to temp slot
	// slot is uint16, so use it's max value
	tempSlot := 65535

	_, err = tx.Exec(
		"UPDATE characters_equipment SET slot = ? WHERE id = ? AND slot = ?",
		tempSlot, r.Id, oldSlot,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"UPDATE characters_equipment SET slot = ? WHERE id = ? AND slot = ?",
		oldSlot, r.Id, newSlot,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"UPDATE characters_equipment SET slot = ? WHERE id = ? AND slot = ?",
		newSlot, r.Id, tempSlot,
	)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

func MoveEquipmentItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	result, err := db.MustExec(
		"UPDATE characters_equipment SET slot = ? "+
			"WHERE id = ? AND slot = ?",
		r.NewItem.Slot, r.Id, r.Item.Slot).RowsAffected()
	if err != nil {
		return err
	}
	if result == 0 {
		return errors.New("equipment item was not found")
	}

	s.Result = true

	return nil
}

func AddItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	_, err := db.MustExec("INSERT INTO characters_inventory "+
		"(id, kind, serials, opt, slot, expire) "+
		"VALUES (?, ?, ?, ?, ?, ?)",
		r.Id, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Slot, r.Item.Expire).RowsAffected()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

func PurchaseItem(_ *rpc.Client, r *inventory.PurchaseRequest, s *inventory.PurchaseResponse) error {
	db := g_DatabaseManager.Get(r.Server)
	s.Result = false
	if r.Price == 0 || r.Item.Kind == 0 {
		return nil
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var alz uint64
	if err := tx.Get(&alz, "SELECT alz FROM characters WHERE id = ? FOR UPDATE", r.Character); err != nil {
		return err
	}
	s.Alz = alz
	if alz < r.Price {
		return nil
	}

	var occupied int
	if err := tx.Get(&occupied,
		"SELECT COUNT(*) FROM characters_inventory WHERE id = ? AND slot = ?",
		r.Character, r.Item.Slot); err != nil {
		return err
	}
	if occupied != 0 {
		return nil
	}

	if _, err := tx.Exec(
		"INSERT INTO characters_inventory (id, kind, serials, opt, slot, expire) "+
			"VALUES (?, ?, ?, ?, ?, ?)",
		r.Character, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Slot, r.Item.Expire); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE characters SET alz = alz - ? WHERE id = ?", r.Price, r.Character); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	s.Alz = alz - r.Price
	return nil
}

func SellItems(_ *rpc.Client, r *inventory.SellRequest, s *inventory.SellResponse) error {
	if r == nil || r.Character <= 0 {
		return errors.New("invalid sell request")
	}

	db := g_DatabaseManager.Get(r.Server)
	s.Result = false
	if r.Price == 0 || len(r.Items) == 0 {
		return nil
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var alz uint64
	if err := tx.Get(&alz, "SELECT alz FROM characters WHERE id = ? FOR UPDATE", r.Character); err != nil {
		return err
	}
	s.Alz = alz
	if ^uint64(0)-alz < r.Price {
		return nil
	}

	seenSlots := make(map[uint16]struct{}, len(r.Items))
	for _, item := range r.Items {
		if item.Kind == 0 {
			return nil
		}
		if _, exists := seenSlots[item.Slot]; exists {
			return nil
		}
		seenSlots[item.Slot] = struct{}{}

		var stored inventory.Item
		if err := tx.Get(&stored,
			"SELECT kind, serials, opt, slot, expire FROM characters_inventory "+
				"WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ? FOR UPDATE",
			r.Character, item.Slot, item.Kind, item.Serials, item.Option, item.Expire); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}

		result, err := tx.Exec(
			"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
			r.Character, item.Slot, item.Kind, item.Serials, item.Option, item.Expire)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return nil
		}
	}

	newAlz := alz + r.Price
	if _, err := tx.Exec("UPDATE characters SET alz = ? WHERE id = ?", newAlz, r.Character); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	s.Alz = newAlz
	return nil
}

func StackItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	_, err := db.MustExec("UPDATE characters_inventory "+
		"SET opt = ? "+
		"WHERE id = ? AND kind = ?",
		r.Item.Option, r.Id, r.Item.Kind).RowsAffected()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

// ConsumeItem atomically decrements one stackable item or removes it when
// the consumed unit was the last one in the stack.
func ConsumeItem(_ *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	s.Result = false
	if r == nil || r.NewItem == nil || r.Id <= 0 || r.Item.Kind == 0 ||
		r.Item.Option <= 0 || r.NewItem.Option != r.Item.Option-1 {
		return nil
	}

	db := g_DatabaseManager.Get(r.Server)
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var stored inventory.Item
	if err := tx.Get(&stored,
		"SELECT kind, serials, opt, slot, expire FROM characters_inventory "+
			"WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ? FOR UPDATE",
		r.Id, r.Item.Slot, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Expire); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if r.NewItem.Option == 0 {
		_, err = tx.Exec(
			"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
			r.Id, r.Item.Slot, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Expire)
	} else {
		_, err = tx.Exec(
			"UPDATE characters_inventory SET opt = ? WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
			r.NewItem.Option, r.Id, r.Item.Slot, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Expire)
	}
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	return nil
}

func RemoveItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	_, err := db.MustExec(
		"DELETE FROM characters_inventory WHERE id = ? AND slot = ?",
		r.Id, r.Item.Slot).RowsAffected()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

func SwapItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	if r.NewItem == nil {
		return errors.New("target item is not set!")
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	// the rollback will be ignored if the tx has been committed later in the function
	defer tx.Rollback()

	// slots were swapped before
	newSlot := r.Item.Slot
	oldSlot := r.NewItem.Slot

	// since id + slot is primary key, we need to switch to temp slot
	// slot is uint16, so use it's max value
	tempSlot := 65535

	_, err = tx.Exec(
		"UPDATE characters_inventory SET slot = ? WHERE id = ? AND slot = ?",
		tempSlot, r.Id, oldSlot,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"UPDATE characters_inventory SET slot = ? WHERE id = ? AND slot = ?",
		oldSlot, r.Id, newSlot,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		"UPDATE characters_inventory SET slot = ? WHERE id = ? AND slot = ?",
		newSlot, r.Id, tempSlot,
	)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

func MoveItem(c *rpc.Client, r *inventory.ItemRequest, s *inventory.ItemResponse) error {
	var db = g_DatabaseManager.Get(r.Server)

	s.Result = false

	_, err := db.MustExec(
		"UPDATE characters_inventory SET slot = ? "+
			"WHERE id = ? AND slot = ?",
		r.NewItem.Slot, r.Id, r.Item.Slot).RowsAffected()
	if err != nil {
		return err
	}

	s.Result = true

	return nil
}

func StorageMove(_ *rpc.Client, r *inventory.StorageMoveRequest, s *inventory.StorageMoveResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 {
		return errors.New("invalid storage move request")
	}

	sourceTable, ok := storageTable(r.SourceType)
	if !ok {
		return errors.New("invalid source storage type")
	}
	targetTable, ok := storageTable(r.TargetType)
	if !ok {
		return errors.New("invalid target storage type")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	if r.SourceType == r.TargetType && r.SourceSlot == r.TargetSlot {
		s.Result = true
		return nil
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var item inventory.Item
	if err = tx.Get(&item,
		"SELECT kind, serials, opt, slot, expire FROM "+sourceTable+
			" WHERE id = ? AND slot = ? FOR UPDATE",
		r.Character, r.SourceSlot); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	var occupied int
	if err = tx.Get(&occupied,
		"SELECT COUNT(*) FROM "+targetTable+" WHERE id = ? AND slot = ?",
		r.Character, r.TargetSlot); err != nil {
		return err
	}
	if occupied != 0 {
		return nil
	}

	if sourceTable == targetTable {
		result, err := tx.Exec(
			"UPDATE "+sourceTable+" SET slot = ? WHERE id = ? AND slot = ?",
			r.TargetSlot, r.Character, r.SourceSlot)
		if err != nil {
			return err
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return nil
		}
	} else {
		item.Slot = r.TargetSlot
		if _, err = tx.Exec(
			"INSERT INTO "+targetTable+
				" (id, kind, serials, opt, slot, expire) VALUES (?, ?, ?, ?, ?, ?)",
			r.Character, item.Kind, item.Serials, item.Option, item.Slot, item.Expire); err != nil {
			return err
		}

		result, err := tx.Exec(
			"DELETE FROM "+sourceTable+" WHERE id = ? AND slot = ?",
			r.Character, r.SourceSlot)
		if err != nil {
			return err
		}

		rows, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return errors.New("source storage item disappeared during move")
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	return nil
}

func storageTable(storageType byte) (string, bool) {
	switch storageType {
	case 0:
		return "characters_inventory", true
	case 2:
		return "characters_warehouse", true
	default:
		return "", false
	}
}
