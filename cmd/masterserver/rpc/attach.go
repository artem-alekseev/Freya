package rpc

import (
	"database/sql"
	"errors"

	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/rpc"
)

// AttachItem atomically changes the target item and removes the source item.
// Both complete item snapshots are checked while their rows are locked, so a
// stale client packet cannot consume or transform a newer item.
func AttachItem(_ *rpc.Client, r *inventory.AttachRequest, s *inventory.AttachResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Source.Kind == 0 || r.Target.Kind == 0 ||
		r.Source.Slot == r.Target.Slot {
		return nil
	}

	newTarget := r.Target
	if r.NewTarget != nil {
		newTarget = *r.NewTarget
	} else {
		if r.NewKind == 0 {
			return nil
		}
		newTarget.Kind = r.NewKind
	}
	if newTarget.Kind == 0 || newTarget.Slot != r.Target.Slot ||
		newTarget.Serials != r.Target.Serials || newTarget.Expire != r.Target.Expire {
		return nil
	}

	db := g_DatabaseManager.Get(r.Server)
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	load := func(slot uint16) (inventory.Item, error) {
		var item inventory.Item
		err := tx.Get(&item,
			"SELECT kind, serials, opt, slot, expire FROM characters_inventory "+
				"WHERE id = ? AND slot = ? FOR UPDATE", r.Character, slot)
		return item, err
	}
	check := func(expected inventory.Item) (inventory.Item, bool, error) {
		actual, err := load(expected.Slot)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return inventory.Item{}, false, nil
			}
			return inventory.Item{}, false, err
		}
		return actual, actual == expected, nil
	}

	// Always lock rows in slot order to keep concurrent opposite-direction
	// attach requests from waiting on each other.
	first, second := r.Source, r.Target
	if first.Slot > second.Slot {
		first, second = second, first
	}
	firstActual, firstMatches, err := check(first)
	if err != nil || !firstMatches {
		return err
	}
	secondActual, secondMatches, err := check(second)
	if err != nil || !secondMatches {
		return err
	}
	if first.Slot == r.Source.Slot {
		if firstActual != r.Source || secondActual != r.Target {
			return nil
		}
	} else if firstActual != r.Target || secondActual != r.Source {
		return nil
	}

	result, err := tx.Exec(
		"UPDATE characters_inventory SET kind = ?, opt = ? WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
		newTarget.Kind, newTarget.Option, r.Character, r.Target.Slot, r.Target.Kind, r.Target.Serials, r.Target.Option, r.Target.Expire,
	)
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

	result, err = tx.Exec(
		"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
		r.Character, r.Source.Slot, r.Source.Kind, r.Source.Serials, r.Source.Option, r.Source.Expire,
	)
	if err != nil {
		return err
	}
	rows, err = result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return nil
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = true
	return nil
}
