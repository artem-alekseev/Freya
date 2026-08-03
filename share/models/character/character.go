package character

import (
	"time"

	"github.com/ubis/Freya/share/models/cashinventory"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/models/skills"
)

type ListReq struct {
	Account int32
	Server  byte
}

type ListRes struct {
	List      []Character
	LastId    int32
	SlotOrder int32
}

type CreateReq struct {
	Server byte
	Character
}

type CreateRes struct {
	Result byte
	Character
}

type SetPremiumReq struct {
	Server      byte
	Character   int32
	ServiceKind byte
}

type SetPremiumRes struct {
	Result      bool
	ServiceKind byte
	Expire      uint64
}

type DeleteReq struct {
	Server byte
	CharId int32
}

type DeleteRes struct {
	Result byte
}

type SetOrderReq struct {
	Server  byte
	Account int32
	Order   int32
}

type SetOrderRes struct {
	Result bool
}

type Character struct {
	Id             int32
	Name           string
	Level          uint16
	World          byte
	X              byte
	Y              byte
	Style          Style
	LiveStyle      int32 `db:"-"`
	Alz            uint64
	Nation         byte   `db:"nation"`
	SwordRank      byte   `db:"sword_rank"`
	MagicRank      byte   `db:"magic_rank"`
	SwordExp       uint16 `db:"sword_exp"`
	MagicExp       uint16 `db:"magic_exp"`
	SwordPoint     uint16 `db:"sword_point"`
	MagicPoint     uint16 `db:"magic_point"`
	SwordRankExp   uint16 `db:"sword_rank_exp"`
	MagicRankExp   uint16 `db:"magic_rank_exp"`
	CurrentHP      uint16 `db:"current_hp"`
	MaxHP          uint16 `db:"max_hp"`
	CurrentMP      uint16 `db:"current_mp"`
	MaxMP          uint16 `db:"max_mp"`
	CurrentSP      uint16 `db:"current_sp"`
	MaxSP          uint16 `db:"max_sp"`
	STR            uint32 `db:"str_stat"`
	INT            uint32 `db:"int_stat"`
	DEX            uint32 `db:"dex_stat"`
	PNT            uint32 `db:"pnt_stat"`
	Exp            uint64
	WarExp         uint64 `db:"war_exp"`
	PremiumService byte   `db:"premium_service"`
	PremiumExpire  uint64 `db:"premium_expire"`
	Equipment      inventory.Equipment
	Inventory      *inventory.Inventory
	Skills         skills.SkillList
	Links          *skills.Links
	Created        time.Time

	// movement data
	BeginX int16 `db:"-"`
	BeginY int16 `db:"-"`
	EndX   int16 `db:"-"`
	EndY   int16 `db:"-"`
}

// HasPremium reports whether the character has an active general premium
// service. PremiumExpire is a Unix timestamp; zero means that the service is
// not active.
func (c Character) HasPremium(now uint64) bool {
	return c.PremiumService != 0 && c.PremiumExpire > now
}

type DataReq struct {
	Server byte
	Id     int32
}

const QuestSlotCount = 10

type DataRes struct {
	Inventory     inventory.Inventory
	Warehouse     inventory.Inventory
	CashInventory []cashinventory.Item
	Quests        []ActiveQuest
	Skills        skills.SkillList
	Links         skills.Links
}

type ActiveQuest struct {
	QuestID        uint16 `db:"quest_id"`
	Slot           byte   `db:"slot"`
	TransmuterSlot uint16 `db:"transmuter_slot"`
	ShowDesc       byte   `db:"show_desc"`
	Expand         byte   `db:"expand"`
	NPCFlags       uint16 `db:"npc_flags"`
}

type OpenQuestReq struct {
	Server    byte
	Character int32
	Quest     ActiveQuest
}

type OpenQuestRes struct {
	Result bool
}

type QuestUIRequest struct {
	Server    byte
	Character int32
	Quest     ActiveQuest
}

type QuestUIResponse struct {
	Result bool
}

type SaveQuestNPCFlagsReq struct {
	Server           byte
	Character        int32
	Quest            ActiveQuest
	ExpectedNPCFlags uint16
}

type SaveQuestNPCFlagsRes struct {
	Result bool
}

type CloseQuestReq struct {
	Server    byte
	Character int32
	Quest     ActiveQuest
	Alz       uint64
}

type CloseQuestRes struct {
	Result bool
}

type ExperienceReq struct {
	Server        byte
	Character     int32
	ExpectedExp   uint64
	ExpectedLevel uint16
	Exp           uint64
	Level         uint16
	StatPoints    uint32
	CurrentHP     uint16
	MaxHP         uint16
	CurrentMP     uint16
	MaxMP         uint16
	CurrentSP     uint16
	MaxSP         uint16
}

type ExperienceRes struct {
	Result bool
}

type SkillExperienceReq struct {
	Server               byte
	Character            int32
	ExpectedSwordRank    byte
	ExpectedMagicRank    byte
	ExpectedSwordExp     uint16
	ExpectedMagicExp     uint16
	ExpectedSwordPoint   uint16
	ExpectedMagicPoint   uint16
	ExpectedSwordRankExp uint16
	ExpectedMagicRankExp uint16
	ExpectedSTR          uint32
	ExpectedDEX          uint32
	ExpectedINT          uint32
	ExpectedCurrentHP    uint16
	ExpectedMaxHP        uint16
	ExpectedCurrentMP    uint16
	ExpectedMaxMP        uint16
	SwordRank            byte
	MagicRank            byte
	SwordExp             uint16
	MagicExp             uint16
	SwordPoint           uint16
	MagicPoint           uint16
	SwordRankExp         uint16
	MagicRankExp         uint16
	STR                  uint32
	DEX                  uint32
	INT                  uint32
	CurrentHP            uint16
	MaxHP                uint16
	CurrentMP            uint16
	MaxMP                uint16
}

type SkillExperienceRes struct {
	Result bool
}

type StatRequest struct {
	Server      byte
	Character   int32
	Stat        byte
	ExpectedPNT uint32
}

type StatResponse struct {
	Result bool
	STR    uint32
	DEX    uint32
	INT    uint32
	PNT    uint32
}

type StatDistributionRequest struct {
	Server      byte
	Character   int32
	STR         uint32
	DEX         uint32
	INT         uint32
	ExpectedPNT uint32
}

type StatDistributionResponse struct {
	Result bool
	STR    uint32
	DEX    uint32
	INT    uint32
	PNT    uint32
}

type VitalsRequest struct {
	Server    byte
	Character int32
	CurrentHP uint16
	MaxHP     uint16
	CurrentMP uint16
	MaxMP     uint16
	CurrentSP uint16
	MaxSP     uint16
}

type VitalsResponse struct {
	Result bool
}

type PositionReq struct {
	Server    byte
	Character int32
	World     byte
	X         byte
	Y         byte
}

type PositionRes struct {
	Result bool
}
