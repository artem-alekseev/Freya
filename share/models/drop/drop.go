package drop

// Entry describes one independent drop roll for a mob species.
// ChanceBPS is expressed in basis points: 10000 means 100%, 100 means 1%.
type Entry struct {
	MobSpecies uint32 `db:"mob_species"`
	ItemKind   uint32 `db:"item_kind"`
	ItemOption int32  `db:"item_option"`
	Amount     uint16 `db:"amount"`
	ChanceBPS  uint16 `db:"chance_bps"`
}

type ListRequest struct {
	Server byte
}

type ListResponse struct {
	Entries []Entry
}
