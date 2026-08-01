package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	statSTR byte = iota
	statDEX
	statINT
)

func UseStatBons(session *network.Session, reader *network.Reader) {
	rawStat := reader.ReadByte()
	log.Debugf("[USESTATBONS] received bStatus=%d", rawStat)
	stat, ok := decodeStat(rawStat)
	if !ok {
		sendUseStatBonsResult(session, false)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Errorf("[USESTATBONS] %s", err)
		sendUseStatBonsResult(session, false)
		return
	}

	ctx.Mutex.RLock()
	if ctx.Char == nil {
		ctx.Mutex.RUnlock()
		sendUseStatBonsResult(session, false)
		return
	}
	characterID := ctx.Char.Id
	expectedPNT := ctx.Char.PNT
	ctx.Mutex.RUnlock()

	if expectedPNT == 0 {
		sendUseStatBonsResult(session, false)
		return
	}

	req := character.StatRequest{
		Server:      byte(g_ServerSettings.ServerId),
		Character:   characterID,
		Stat:        stat,
		ExpectedPNT: expectedPNT,
	}
	res := character.StatResponse{}
	if err := g_RPCHandler.Call(rpc.SaveStat, &req, &res); err != nil {
		log.Errorf("[USESTATBONS] Unable to save stat point for character %d: %s", characterID, err)
		sendUseStatBonsResult(session, false)
		return
	}
	if !res.Result {
		sendUseStatBonsResult(session, false)
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char != nil {
		ctx.Char.STR = res.STR
		ctx.Char.DEX = res.DEX
		ctx.Char.INT = res.INT
		ctx.Char.PNT = res.PNT
	}
	ctx.Mutex.Unlock()

	log.Infof("[USESTATBONS] Character %d increased %s, remaining points: %d",
		characterID, statName(stat), res.PNT)
	sendUseStatBonsResult(session, true)
}

func decodeStat(raw byte) (byte, bool) {
	switch raw {
	case 2: // STAT_STR in the client protocol
		return statSTR, true
	case 3: // STAT_DEX in the client protocol
		return statDEX, true
	case 4: // STAT_INT in the client protocol
		return statINT, true
	default:
		return 0, false
	}
}

func statName(stat byte) string {
	switch stat {
	case statSTR:
		return "STR"
	case statDEX:
		return "DEX"
	case statINT:
		return "INT"
	default:
		return "unknown"
	}
}

func sendUseStatBonsResult(session *network.Session, result bool) {
	pkt := network.NewWriter(USESTATBONS)
	pkt.WriteBool(result)
	session.Send(pkt)
}
