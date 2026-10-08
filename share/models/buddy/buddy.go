package buddy

const (
	ResultOK byte = iota
	ResultUndefined
	ResultInvalidParam
	ResultNotExist
	ResultInvalidReturn
	ResultDBFail
	ResultTargetOffline
	ResultRefuse
	ResultAlreadyMember
	ResultInviting
	ResultInvitingByOther
	ResultMaxMember
	ResultMaxMemberTarget
	ResultBlockedBy
	ResultBlockedTo
)

const (
	DefaultGroup uint32 = 0
	MaxMembers   int    = 300
)

// Character is the character information needed by ChatNode's buddy packets.
// Names cross the RPC boundary as UTF-8 and are converted to the legacy
// client encoding only when a packet is written.
type Character struct {
	ID          int32
	Name        string
	Level       byte
	BattleStyle byte
	Nation      byte
	ClassLevel  byte
	GuildNo     uint32
}

type LookupRequest struct {
	Server      byte
	CharacterID int32
	Name        string
}

type LookupResponse struct {
	Found     bool
	Character Character
}

type ListRequest struct {
	Server    byte
	Character int32
}

type Member struct {
	Character
	Group uint32
	Memo  string
}

type ListResponse struct {
	Members []Member
}

type AddRequest struct {
	Server    byte
	Requester int32
	Target    int32
}

type AddResponse struct {
	Result byte
}

// MemoModifyResult values are the result codes used by
// CSC_BUDDYMEMBERMEMOMODIFY (470). They intentionally follow WorldSvr's
// protocol values and are separate from the invitation result codes above.
const (
	MemoModifyOK byte = iota
	MemoModifyInvalidParam
	MemoModifyUndefined
	MemoModifyInvalidReturn
	MemoModifyDBFail
)

type MemoModifyRequest struct {
	Server    byte
	Character int32
	Target    int32
	Memo      string
}

type MemoModifyResponse struct {
	Result byte
	Memo   string
}

// DeleteResult values are the result codes used by CSC_BUDDYMEMBERDEL
// (471). They follow WorldSvr's protocol values and remain separate from
// invitation and memo-modification results.
const (
	DeleteOK byte = iota
	DeleteUndefined
	DeleteInvalidParam
	DeleteNotExist
	DeleteInvalidReturn
	DeleteDBFail
)

type DeleteRequest struct {
	Server    byte
	Character int32
	Target    int32
}

type DeleteResponse struct {
	Result byte
}
