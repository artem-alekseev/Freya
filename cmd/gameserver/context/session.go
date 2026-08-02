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
