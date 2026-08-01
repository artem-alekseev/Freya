package rpc

import (
	"database/sql"
	"errors"

	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/rpc"
)

const (
	upgradeCoreShift = 13
	upgradeCoreMask  = uint32(0x0001E000)
	maxUpgradeLevel  = uint32(15)
)

// EnchantItem applies one upgrade level and consumes all submitted cores in a
// single transaction. The exact item snapshots prevent stale client packets
// from modifying a slot that changed since the request was read.
func EnchantItem(_ *rpc.Client, r *inventory.EnchantRequest, s *inventory.EnchantResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Target.Kind == 0 || r.NewKind == 0 || len(r.Cores) == 0 {
		return nil
	}

	oldLevel := (r.Target.Kind & upgradeCoreMask) >> upgradeCoreShift
	if oldLevel >= maxUpgradeLevel {
		return nil
	}
	expectedKind := (r.Target.Kind &^ upgradeCoreMask) | ((oldLevel + 1) << upgradeCoreShift)
	if r.NewKind != expectedKind {
		return nil
	}

	db := g_DatabaseManager.Get(r.Server)
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var storedTarget inventory.Item
	if err := tx.Get(&storedTarget,
		"SELECT kind, serials, opt, slot, expire FROM characters_inventory "+
			"WHERE id = ? AND slot = ? FOR UPDATE", r.Character, r.Target.Slot); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if storedTarget != r.Target {
		return nil
	}

	seenSlots := make(map[uint16]struct{}, len(r.Cores))
	for _, core := range r.Cores {
		if core.Kind == 0 || core.Slot == r.Target.Slot {
			return nil
		}
		if _, exists := seenSlots[core.Slot]; exists {
			return nil
		}
		seenSlots[core.Slot] = struct{}{}

		var storedCore inventory.Item
		if err := tx.Get(&storedCore,
			"SELECT kind, serials, opt, slot, expire FROM characters_inventory "+
				"WHERE id = ? AND slot = ? FOR UPDATE", r.Character, core.Slot); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if storedCore != core {
			return nil
		}
	}

	result, err := tx.Exec(
		"UPDATE characters_inventory SET kind = ? WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
		r.NewKind, r.Character, r.Target.Slot, r.Target.Kind, r.Target.Serials, r.Target.Option, r.Target.Expire,
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

	for _, core := range r.Cores {
		result, err := tx.Exec(
			"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ?",
			r.Character, core.Slot, core.Kind, core.Serials, core.Option, core.Expire,
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
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = true
	return nil
}
