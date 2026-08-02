package cashinventory

type LoadRequest struct {
	Server    byte
	Character int32
}

type LoadResponse struct {
	Items []Item
}

type AddRequest struct {
	Server     byte
	Character  int32
	Kind       uint32
	Option     int32
	Count      int32
	DurationID byte
}

type AddResponse struct {
	Items []Item
}

type UseRequest struct {
	Server    byte
	Character int32
	CashID    int32
	Slot      uint16
	Item      Item
	Expire    uint32
}
