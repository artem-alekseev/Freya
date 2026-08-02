package rpc

import (
	"errors"

	"github.com/ubis/Freya/share/models/cashinventory"
	"github.com/ubis/Freya/share/rpc"
)

const maxCashItemsPerCommand int32 = 1000

// AddCashItems adds test/admin items to the character's pending cash shop.
// cash_id is scoped to the character and is allocated from the current
// maximum while the character rows are locked by the transaction.
func AddCashItems(_ *rpc.Client, r *cashinventory.AddRequest, s *cashinventory.AddResponse) error {
	s.Items = nil
	if r == nil || r.Character <= 0 || r.Kind == 0 || r.Count <= 0 || r.Count > maxCashItemsPerCommand {
		return errors.New("invalid cash item add request")
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

	var nextID int32
	if err := tx.Get(&nextID,
		"SELECT COALESCE(MAX(cash_id), 0) + 1 FROM characters_cash_inventory WHERE id = ? FOR UPDATE",
		r.Character); err != nil {
		return err
	}

	s.Items = make([]cashinventory.Item, 0, r.Count)
	for index := int32(0); index < r.Count; index++ {
		item := cashinventory.Item{
			ID:         nextID + index,
			Kind:       r.Kind,
			Option:     r.Option,
			DurationID: r.DurationID,
		}
		if _, err := tx.Exec(
			"INSERT INTO characters_cash_inventory (id, cash_id, kind, opt, duration_idx) VALUES (?, ?, ?, ?, ?)",
			r.Character, item.ID, item.Kind, item.Option, item.DurationID); err != nil {
			return err
		}
		s.Items = append(s.Items, item)
	}

	return tx.Commit()
}
