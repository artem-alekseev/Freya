package mail

import "github.com/ubis/Freya/share/models/inventory"

const (
	TypeUser     byte = 1
	TypeAttached byte = 2
	TypeReturned byte = 3
	TypeGM       byte = 4

	StateRead         byte = 1
	StateAlzReceived  byte = 2
	StateItemReceived byte = 4
	// StateLocked is kept as a mask for operations that must be blocked after
	// either attachment has been received, such as returning the mail.
	StateLocked byte = StateAlzReceived | StateItemReceived

	AttachmentFee uint64 = 10000
	MaxMailbox    int    = 100
	MaxTitle      int    = 40
	MaxContent    int    = 512
)

const (
	SendOK byte = iota
	SendFail
	SendTooLowLevel
	SendNotAttachableItem
	SendNotEnoughAlz
	SendNotEnoughFee
	SendNotFoundCharacter
	SendSameAccount
	SendBlocked
	SendFullMailbox
)

const (
	ReturnOK byte = iota
	ReturnFail
	ReturnLocked
	ReturnNotFoundCharacter
	ReturnFullMailbox
	ReturnNotAttachable
	ReturnAlreadyReceivedAttachment
)

const (
	DeleteOK byte = iota
	DeleteFail
	DeleteExistingAttachment
)

// Summary is the common, variable-free representation used between services.
// DeliveryTime is a Unix timestamp, matching ChatNode's cbtime_t on the wire.
type Summary struct {
	ID           int32
	Type         byte
	DeliveryTime int64
	State        byte
	SenderName   string
	ReceiverName string
	Title        string
}

type Detail struct {
	Alz     uint64
	Item    inventory.Item
	HasItem bool
	Content string
}

type ListRequest struct {
	Server     byte
	Character  int32
	LastMailID int32
}

type ListResponse struct {
	Received []Summary
	Sent     []Summary
	New      []Summary
}

// NewReceivedRequest is sent to ChatServer when an online character receives
// a new mail and must be notified immediately.
type NewReceivedRequest struct {
	Server    byte
	Character int32
}

type NewReceivedResponse struct{}

type DetailRequest struct {
	Server    byte
	Character int32
	MailID    int32
	Sent      bool
}

type DetailResponse struct {
	Result  bool
	Summary Summary
	Detail  Detail
}

type ReadRequest struct {
	Server    byte
	Character int32
	MailID    int32
}

type ReadResponse struct {
	Result bool
}

type SendRequest struct {
	Server       byte
	Sender       int32
	SenderName   string
	ReceiverName string
	ReplyID      int32
	Title        string
	Content      string
	Alz          uint64
	Item         inventory.Item
	HasItem      bool
}

type SendResponse struct {
	Result       bool
	ResultCode   byte
	MailID       int32
	ReceiverName string
	CurrentAlz   uint64
}

type ActionRequest struct {
	Server    byte
	Character int32
	MailID    int32
	Slot      uint16
}

type ActionResponse struct {
	Result       bool
	ResultCode   byte
	MailID       int32
	SentMailID   int32
	CurrentAlz   uint64
	Item         inventory.Item
	HasItem      bool
	ReceiverName string
}
