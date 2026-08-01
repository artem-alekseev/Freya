package context

type ItemHandler interface {
	GetId() int32
	GetOwner() int32
	GetSource() int32
	GetDropType() byte
	GetDropInfo() byte
	GetKind() uint32
	GetOption() int32
	GetPosition() (uint16, uint16)
	GetKey() uint16
	IsOwnerExpired() bool
}
