package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

// SaveCharacterPosition persists the last known position of the character in
// the session. It is safe to call when the session is already in the lobby.
func SaveCharacterPosition(session *network.Session) {
	ctx, err := context.Parse(session)
	if err != nil {
		return
	}

	ctx.Mutex.RLock()
	req := character.PositionReq{
		Server:    byte(g_ServerSettings.ServerId),
		Character: ctx.Char.Id,
		World:     ctx.Char.World,
		X:         ctx.Char.X,
		Y:         ctx.Char.Y,
	}
	ctx.Mutex.RUnlock()

	var res character.PositionRes
	if err := g_RPCHandler.Call(rpc.SavePosition, &req, &res); err != nil {
		log.Errorf("Failed to save character %d position: %s", req.Character, err)
		return
	}
	if !res.Result {
		log.Errorf("Position of character %d was not saved", req.Character)
	}
}
