package context

import (
	"errors"
	"sync"
	"time"

	"github.com/ubis/Freya/share/models/cashinventory"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
)

// Context holds information related to the player's current context within the game.
type Context struct {
	Mutex                 sync.RWMutex
	Char                  *character.Character
	Warehouse             inventory.Inventory
	CashInventory         cashinventory.Inventory
	PVP                   PVPState
	Trade                 TradeState
	Party                 PartyState
	PartyInvite           PartyInviteState
	PartySearch           PartySearchState
	BattleMode            BattleModeState
	BattleModeGeneration  uint64
	BattleModeEndTimer    *time.Timer
	ComboActive           bool
	BuffGeneration        uint64
	Buffs                 []ActiveBuff
	MPPotionCooldownUntil time.Time
	ActiveQuests          [QuestSlotCount]ActiveQuest

	Cell         CellHandler
	World        WorldHandler
	WorldManager WorldManagerHandler
}

// PVPState stores the in-memory state of a personal PvP handshake.
// Type is 0 when the character is not participating in a PvP session,
// 1 while waiting for the other player, and 2 after the request is accepted.
type PVPState struct {
	Type                byte
	OpponentUserIdx     uint16
	OpponentCharacterID int32
}

// PartyState stores the active party for the current game session. Party
// membership is runtime state for now; PartySvr persistence can be added
// later without changing the client packet layer.
type PartyState struct {
	ID                        int32
	LeaderUserIdx             uint16
	LeaderCharacterID         int32
	LeaderOnlyInviteAuthority bool
	NormalLootingType         byte
	OwnerLootingType          byte
	Members                   []PartyMember
}

type PartyMember struct {
	UserIdx      uint16
	CharacterID  int32
	Level        uint16
	Channel      byte
	BattleStyle  byte
	MemberStatus byte
	Name         string
}

type PartyInviteState struct {
	Status              byte
	OpponentUserIdx     uint16
	OpponentCharacterID int32
	Channel             byte
}

// PartySearchState stores the temporary party-search advertisement for the
// current character. It is intentionally runtime-only, like PartyState.
type PartySearchState struct {
	Registered     bool
	MaxMemberCount byte
	Title          string
}

const (
	PartyInviteNone     byte = 0
	PartyInviteOutgoing byte = 1
	PartyInviteIncoming byte = 2
)

const (
	PartyMemberStatusNone   byte = 0
	PartyMemberStatusLogin  byte = 1
	PartyMemberStatusLogoff byte = 2
)

const PartyMaxMembers = 7

// TradeState stores the in-memory state of a trade request and exchange.
type TradeState struct {
	Status              byte
	Role                byte
	OpponentUserIdx     uint16
	OpponentCharacterID int32
	Items               []TradeItemOffer
	Alz                 uint64
	Submitted           bool
	DestinationSlots    []uint16
}

// TradeItemOffer keeps the source inventory item together with the slot it
// occupies in the trade window. The source slot remains part of Item.Slot.
type TradeItemOffer struct {
	Item      inventory.Item
	TradeSlot uint16
}

const (
	TradeStateNone    byte = 0
	TradeStatePending byte = 1
	TradeStateOpen    byte = 2
)

const (
	TradeRoleNone      byte = 0
	TradeRoleRequester byte = 1
	TradeRoleTarget    byte = 2
)

// Init initializes a new context for the session.
func Init(session *network.Session) {
	session.DataEx = &Context{}
}

// PreParse retrieves the context from the session before character data is set.
func PreParse(session *network.Session) (*Context, error) {
	err := errors.New("unable to parse session context")

	if session.DataEx == nil {
		// we have invalid session, ignore
		return nil, err
	}

	ctx, ok := session.DataEx.(*Context)
	if !ok {
		// we have invalid session, ignore
		return nil, err
	}

	return ctx, nil
}

// Parse retrieves the context from the session.
// It ensures that the context is valid, the character is set.
func Parse(session *network.Session) (*Context, error) {
	err := errors.New("unable to parse session context")

	if session.DataEx == nil {
		// we have invalid session, ignore
		return nil, err
	}

	ctx, ok := session.DataEx.(*Context)
	if !ok {
		// we have invalid session, ignore
		return nil, err
	}

	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	if ctx.Char == nil {
		// session is in the lobby, we cannot receive such messages
		return nil, err
	}

	return ctx, nil
}
