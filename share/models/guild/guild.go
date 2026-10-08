package guild

const (
	CreateOK byte = iota
	CreateUndefined
	CreateCharacterLevel
	CreateHasGuild
	CreateGuildExists
	CreateInvalidParam
	CreateUndefinedReturnCode
	CreateDBFail
	CreateInvalidReturn
	CreateDBFetchRow
	CreateNotEnoughMoney
	CreateBadWord
)

const (
	// These values match GUILDCREATE in server8files/Share/Typedef.h.
	MinCreateLevel uint16 = 40
	MaxNameBytes          = 20
)

type CreateRequest struct {
	Server    byte
	Character int32
	Name      string
}

type CreateResponse struct {
	GuildNo uint32
	Result  byte
}
