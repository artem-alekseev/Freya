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

// UserStatAutoDistribution applies a batch of free stat points. The client
// sends the number of points for STR, DEX and INT as three int32 values, and
// expects the applied amounts back in the same order.
func UserStatAutoDistribution(session *network.Session, reader *network.Reader) {
	statSTR := reader.ReadInt32()
	statDEX := reader.ReadInt32()
	statINT := reader.ReadInt32()
	log.Debugf("[USERSTAT_AUTODISTRIBUTION] received STR=%d DEX=%d INT=%d",
		statSTR, statDEX, statINT)

	if statSTR < 0 || statDEX < 0 || statINT < 0 {
		log.Warning("[USERSTAT_AUTODISTRIBUTION] rejected negative stat distribution")
		sendStatDistributionResult(session, 0, 0, 0)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Errorf("[USERSTAT_AUTODISTRIBUTION] %s", err)
		sendStatDistributionResult(session, 0, 0, 0)
		return
	}

	ctx.Mutex.RLock()
	if ctx.Char == nil {
		ctx.Mutex.RUnlock()
		sendStatDistributionResult(session, 0, 0, 0)
		return
	}
	characterID := ctx.Char.Id
	expectedPNT := ctx.Char.PNT
	ctx.Mutex.RUnlock()

	req := character.StatDistributionRequest{
		Server:      byte(g_ServerSettings.ServerId),
		Character:   characterID,
		STR:         uint32(statSTR),
		DEX:         uint32(statDEX),
		INT:         uint32(statINT),
		ExpectedPNT: expectedPNT,
	}
	res := character.StatDistributionResponse{}
	if err := g_RPCHandler.Call(rpc.SaveStatDistribution, &req, &res); err != nil {
		log.Errorf("[USERSTAT_AUTODISTRIBUTION] unable to save stats for character %d: %s", characterID, err)
		sendStatDistributionResult(session, 0, 0, 0)
		return
	}
	if !res.Result {
		log.Warningf("[USERSTAT_AUTODISTRIBUTION] rejected distribution for character %d: requested=%d/%d/%d points=%d",
			characterID, statSTR, statDEX, statINT, expectedPNT)
		sendStatDistributionResult(session, 0, 0, 0)
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

	log.Infof("[USERSTAT_AUTODISTRIBUTION] Character %d added STR=%d DEX=%d INT=%d, remaining points: %d",
		characterID, statSTR, statDEX, statINT, res.PNT)
	sendStatDistributionResult(session, statSTR, statDEX, statINT)
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

func sendStatDistributionResult(session *network.Session, statSTR, statDEX, statINT int32) {
	pkt := network.NewWriter(USERSTAT_AUTODISTRIBUTION)
	pkt.WriteInt32(statSTR)
	pkt.WriteInt32(statDEX)
	pkt.WriteInt32(statINT)
	session.Send(pkt)
}
