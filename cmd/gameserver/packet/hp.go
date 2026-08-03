package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	updateTypeHP           byte = 3
	updateTypeMP           byte = 4
	updateTypeMPPotion     byte = 2
	updateTypeSP           byte = 5
	updateTypeRank         byte = 9
	updateTypeSPDrainEx    byte = 11
	updateTypeSPDeficiency byte = 17

	battleModeErrorMPInsufficiency uint16 = 14
	battleModeErrorSPDeficiency    uint16 = 17
)

func saveCharacterVitals(c *character.Character) bool {
	if c == nil {
		return false
	}

	req := character.VitalsRequest{
		Server:    byte(g_ServerSettings.ServerId),
		Character: c.Id,
		CurrentHP: c.CurrentHP,
		MaxHP:     c.MaxHP,
		CurrentMP: c.CurrentMP,
		MaxMP:     c.MaxMP,
		CurrentSP: c.CurrentSP,
		MaxSP:     c.MaxSP,
	}
	res := character.VitalsResponse{}
	if err := g_RPCHandler.Call(rpc.SaveVitals, &req, &res); err != nil {
		log.Errorf("Unable to save vitals for character %d: %s", c.Id, err)
		return false
	}
	if !res.Result {
		log.Errorf("Master rejected vitals update for character %d", c.Id)
		return false
	}
	return true
}

func saveContextVitals(ctx *context.Context) bool {
	if ctx == nil {
		return false
	}

	ctx.Mutex.RLock()
	if ctx.Char == nil {
		ctx.Mutex.RUnlock()
		return false
	}
	snapshot := *ctx.Char
	ctx.Mutex.RUnlock()

	return saveCharacterVitals(&snapshot)
}

// sendManaUpdate follows the client's NFY_UPDATEDATAS layout for UT_MP,
// whose value is stored in the union's integer field.
func sendManaUpdate(session *network.Session, currentMP uint16) {
	sendUpdatedData(session, updateTypeMP, uint64(currentMP))
	notifyPartyMemberStats(session)
}

func sendManaPotionUpdate(session *network.Session, currentMP uint16) {
	sendUpdatedData(session, updateTypeMPPotion, uint64(currentMP))
	notifyPartyMemberStats(session)
}

func sendSpiritUpdate(session *network.Session, currentSP uint16) {
	sendUpdatedData(session, updateTypeSP, uint64(currentSP))
	notifyPartyMemberStats(session)
}

// sendBattleModeFailure follows WorldSvr's failure path. WorldSvr does not
// answer SkillToUser with S2C_SKILLTOUSER when MP/SP is insufficient: it sends
// NFS_UPDATEDATAS for SP deficiency and/or NFS_LASTERRCODE instead.
func sendBattleModeFailure(session *network.Session, state battleModeSkillResult) {
	switch state.Failure {
	case battleModeFailureSpirit:
		sendUpdatedData(session, updateTypeSPDeficiency, uint64(state.CurrentSP))
		sendLastErrorCode(session, SKILLTOUSER, 0, battleModeErrorSPDeficiency)
	case battleModeFailureMana:
		sendLastErrorCode(session, SKILLTOUSER, uint16(battleModeSkillGroup), battleModeErrorMPInsufficiency)
	}
}

// sendLastErrorCode is the NFS_LASTERRCODE layout from WorldSvr::NotifyLastErrCode:
// request command, request sub-command, error code, and dead type.
func sendLastErrorCode(session *network.Session, requestCommand, requestSubCommand, errorCode uint16) {
	pkt := network.NewWriter(NFY_LASTERRCODE)
	pkt.WriteUint16(requestCommand)
	pkt.WriteUint16(requestSubCommand)
	pkt.WriteUint16(errorCode)
	pkt.WriteUint16(0) // DeadType::DT_NONE
	session.Send(pkt)
}

// sendHealthUpdate follows the client's NFY_UPDATEDATAS layout: for UT_HP,
// the current HP is stored in the second WORD of the value union.
func sendHealthUpdate(session *network.Session, currentHP uint16) {
	pkt := network.NewWriter(NFY_UPDATEDATAS)
	pkt.WriteByte(updateTypeHP)
	pkt.WriteUint16(0) // Soul Ability Points
	pkt.WriteUint32(0) // Soul Ability EXP
	pkt.WriteUint16(0) // damage
	pkt.WriteUint16(currentHP)
	pkt.WriteUint32(0)
	session.Send(pkt)
	notifyPartyMemberStats(session)
}
