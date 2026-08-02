package rpc

import (
	"database/sql"
	"errors"
	"sort"

	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/rpc"
)

// ForceCoreEnchant atomically updates the target option, decrements submitted
// scrolls and removes submitted force-core items. Complete snapshots are
// checked while rows are locked so a stale packet cannot spend another item's
// materials.
func ForceCoreEnchant(_ *rpc.Client, r *inventory.ForceCoreEnchantRequest, s *inventory.ForceCoreEnchantResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Target.Kind == 0 || r.NewTarget.Kind == 0 ||
		r.Target.Slot != r.NewTarget.Slot || r.Target.Kind != r.NewTarget.Kind ||
		r.Target.Serials != r.NewTarget.Serials || r.Target.Expire != r.NewTarget.Expire ||
		len(r.Cores) == 0 {
		return nil
	}

	materials := make([]inventory.Item, 0, len(r.Cores)+2)
	if r.RandomScroll != nil {
		materials = append(materials, *r.RandomScroll)
	}
	if r.SpecificScroll != nil {
		materials = append(materials, *r.SpecificScroll)
	}
	materials = append(materials, r.Cores...)
	coreSlots := make(map[uint16]struct{}, len(r.Cores))
	for _, core := range r.Cores {
		coreSlots[core.Slot] = struct{}{}
	}

	seen := map[uint16]struct{}{r.Target.Slot: {}}
	for _, material := range materials {
		if material.Kind == 0 {
			return nil
		}
		if _, exists := seen[material.Slot]; exists {
			return nil
		}
		seen[material.Slot] = struct{}{}
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

	// Lock every row in slot order to avoid deadlocks between simultaneous
	// enchant requests containing overlapping material slots.
	locked := make([]inventory.Item, 0, len(materials)+1)
	locked = append(locked, r.Target)
	locked = append(locked, materials...)
	sort.Slice(locked, func(i, j int) bool { return locked[i].Slot < locked[j].Slot })
	for _, expected := range locked {
		actual, err := load(expected.Slot)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if actual != expected {
			return nil
		}
	}

	var result sql.Result
	var rows int64
	if r.NewTarget.Option != r.Target.Option {
		result, err = tx.Exec(
			"UPDATE characters_inventory SET opt = ? WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
			r.NewTarget.Option, r.Character, r.Target.Slot, r.Target.Kind, r.Target.Serials, r.Target.Option, r.Target.Expire,
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
	}

	for _, material := range materials {
		if _, isCore := coreSlots[material.Slot]; isCore {
			result, err = tx.Exec(
				"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
				r.Character, material.Slot, material.Kind, material.Serials, material.Option, material.Expire,
			)
		} else if material.Option > 0 {
			result, err = tx.Exec(
				"UPDATE characters_inventory SET opt = ? WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
				material.Option-1, r.Character, material.Slot, material.Kind, material.Serials, material.Option, material.Expire,
			)
		} else {
			result, err = tx.Exec(
				"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
				r.Character, material.Slot, material.Kind, material.Serials, material.Option, material.Expire,
			)
		}
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
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = true
	return nil
}
