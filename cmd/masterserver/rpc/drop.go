package rpc

import (
	"errors"

	"github.com/ubis/Freya/share/models/drop"
	"github.com/ubis/Freya/share/rpc"
)

// LoadMobDropList loads enabled mob drop rules from the World database.
func LoadMobDropList(_ *rpc.Client, r *drop.ListRequest, s *drop.ListResponse) error {
	if r == nil {
		return errors.New("invalid mob drop list request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	entries := make([]drop.Entry, 0)
	if err := db.Select(&entries,
		"SELECT mob_species, item_kind, item_option, amount, chance_bps "+
			"FROM mob_drop_list "+
			"WHERE enabled = 1 AND item_kind <> 0 AND amount > 0 "+
			"AND chance_bps > 0 AND chance_bps <= 10000 "+
			"ORDER BY mob_species, id",
	); err != nil {
		return err
	}

	s.Entries = entries
	return nil
}
