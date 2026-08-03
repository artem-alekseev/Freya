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

type StorageMoveRequest struct {
	Server     byte
	Character  int32
	SourceType byte
	SourceSlot uint16
	TargetType byte
	TargetSlot uint16
}

type StorageMoveResponse struct {
	Result bool
}

// TradeItemTransfer describes one item moving from a source character to a
// destination inventory slot during an atomic trade transaction.
type TradeItemTransfer struct {
	Item       Item
	TargetSlot uint16
}

// TradeRequest transfers both sides' items and Alz in one database
// transaction. Items from First are received by Second and vice versa.
type TradeRequest struct {
	Server          byte
	FirstCharacter  int32
	SecondCharacter int32
	FirstItems      []TradeItemTransfer
	SecondItems     []TradeItemTransfer
	FirstAlz        uint64
	SecondAlz       uint64
}

type TradeResponse struct {
	Result    bool
	FirstAlz  uint64
	SecondAlz uint64
}

// EnchantRequest updates one inventory item and consumes the supplied core
// items as one transaction in the World database.
type EnchantRequest struct {
	Server    byte
	Character int32
	Target    Item
	NewKind   uint32
	Cores     []Item
}

type EnchantResponse struct {
	Result bool
}

// ForceCoreEnchantRequest atomically updates the target option and consumes
// the submitted scrolls and force cores.
type ForceCoreEnchantRequest struct {
	Server         byte
	Character      int32
	Target         Item
	NewTarget      Item
	RandomScroll   *Item
	SpecificScroll *Item
	Cores          []Item
}

type ForceCoreEnchantResponse struct {
	Result bool
}

// AttachRequest transforms the target item with the source item and consumes
// the source atomically in the World database.
type AttachRequest struct {
	Server    byte
	Character int32
	Source    Item
	Target    Item
	NewKind   uint32
	NewTarget *Item
}

type AttachResponse struct {
	Result bool
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

type SellRequest struct {
	Server    byte
	Character int32
	Items     []Item
	Price     uint64
}

type SellResponse struct {
	Result bool
	Alz    uint64
}
