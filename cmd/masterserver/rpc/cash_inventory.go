package rpc

import (
	"database/sql"
	"errors"

	"github.com/ubis/Freya/share/models/cashinventory"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/rpc"
)

// LoadCashInventory refreshes the pending cash item list for an online
// character. The same loader is used during character initialization.
func LoadCashInventory(_ *rpc.Client, r *cashinventory.LoadRequest, s *cashinventory.LoadResponse) error {
	s.Items = make([]cashinventory.Item, 0)
	if r == nil || r.Character <= 0 {
		return errors.New("invalid cash inventory load request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	s.Items = loadCashInventory(db, r.Character)
	return nil
}

// UseCashItem atomically removes a pending cash item and creates the
// corresponding item in the character's normal inventory.
func UseCashItem(_ *rpc.Client, r *cashinventory.UseRequest, s *cashinventory.UseResponse) error {
	s.Result = cashinventory.UseDatabaseFailed
	s.Item = inventory.Item{}
	if r == nil || r.Character <= 0 || r.CashID <= 0 || r.Item.Kind == 0 {
		return errors.New("invalid cash item use request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var stored cashinventory.Item
	if err := tx.Get(&stored,
		"SELECT cash_id, kind, opt, duration_idx FROM characters_cash_inventory "+
			"WHERE id = ? AND cash_id = ? FOR UPDATE",
		r.Character, r.CashID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.Result = cashinventory.UseNotFound
			return nil
		}
		return err
	}

	if stored.Kind != r.Item.Kind || stored.Option != r.Item.Option ||
		stored.DurationID != r.Item.DurationID {
		s.Result = cashinventory.UseNotFound
		return nil
	}

	var occupied int
	if err := tx.Get(&occupied,
		"SELECT COUNT(*) FROM characters_inventory WHERE id = ? AND slot = ?",
		r.Character, r.Slot); err != nil {
		return err
	}
	if occupied != 0 {
		s.Result = cashinventory.UseSlotOccupied
		return nil
	}

	item := inventory.Item{
		Kind:   r.Item.Kind,
		Option: r.Item.Option,
		Slot:   r.Slot,
		Expire: r.Expire,
	}
	if _, err := tx.Exec(
		"INSERT INTO characters_inventory (id, kind, serials, opt, slot, expire) "+
			"VALUES (?, ?, ?, ?, ?, ?)",
		r.Character, item.Kind, item.Serials, item.Option, item.Slot, item.Expire); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM characters_cash_inventory WHERE id = ? AND cash_id = ?",
		r.Character, r.CashID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = cashinventory.UseOK
	s.Item = item
	return nil
}
