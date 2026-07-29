package inventory

type ItemRequest struct {
	Server  byte
	Id      int32
	Command string
	Item    Item
	NewItem *Item
}

type ItemResponse struct {
	Result bool
	Item   *Item
}

type PurchaseRequest struct {
	Server    byte
	Character int32
	Item      Item
	Price     uint64
}

type PurchaseResponse struct {
	Result bool
	Alz    uint64
}
