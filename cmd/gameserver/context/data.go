package context

// Warp represents a warp point that allows players to move between worlds and locations.
type Warp struct {
	Id    uint16
	World byte
	// Location is ordered as NATION_NONE, NATION_CAPELLA, NATION_PROCYON.
	Location [3]WarpLocation
	Code     byte
	Fee      uint32
	Level    uint16
}

type WarpLocation struct {
	X byte
	Y byte
}
