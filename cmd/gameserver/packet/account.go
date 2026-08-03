package packet

import (
	"bytes"
	"time"

	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/models/account"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

// ChargeInfo Packet
func ChargeInfo(session *network.Session, reader *network.Reader) {
	sendChargeInfo(session)
}

func chargeState(session *network.Session) (byte, int32) {
	serviceKind := byte(0)
	remaining := int32(0)
	if ctx, err := context.Parse(session); err == nil {
		ctx.Mutex.RLock()
		now := uint64(time.Now().Unix())
		if ctx.Char != nil && ctx.Char.HasPremium(now) {
			serviceKind = ctx.Char.PremiumService
			seconds := ctx.Char.PremiumExpire - now
			if seconds > uint64(^uint32(0)>>1) {
				seconds = uint64(^uint32(0) >> 1)
			}
			remaining = int32(seconds)
		}
		ctx.Mutex.RUnlock()
	}
	return serviceKind, remaining
}

func sendChargeInfo(session *network.Session) {
	serviceKind, remaining := chargeState(session)
	var packet = network.NewWriter(CHARGEINFO)
	// S2C_CHARGEINFO layout used by WorldSvr:
	// bPayMode, iFixedRateTime, iTimeRateTime, serviceKind.
	// The client expects the remaining duration in seconds, not a Unix timestamp.
	if remaining > 0 {
		packet.WriteByte(byte(0x08)) // CHARGING_TYPE::FIXED_RATE
	} else {
		packet.WriteByte(byte(0x00))
	}
	packet.WriteInt32(remaining)
	packet.WriteInt32(int32(0))
	packet.WriteByte(serviceKind)

	session.Send(packet)
}

func sendChargeNotification(session *network.Session) {
	serviceKind, remaining := chargeState(session)
	packet := network.NewWriter(NFY_CHARGEINFO)
	// NFS_CHARGEINFO layout used by WorldSvr:
	// bPayMode, iRemainTime, serviceKind.
	if remaining > 0 {
		packet.WriteByte(byte(0x08)) // CHARGING_TYPE::FIXED_RATE
	} else {
		packet.WriteByte(byte(0x00))
	}
	packet.WriteInt32(remaining)
	packet.WriteByte(serviceKind)

	session.Send(packet)
}

// CheckUserPrivacyData Packet
func CheckUserPrivacyData(session *network.Session, reader *network.Reader) {
	// skip 4 bytes
	reader.ReadInt32()

	var passwd = string(bytes.Trim(reader.ReadBytes(32), "\x00"))

	var req = account.AuthCheckReq{session.Data.AccountId, passwd}
	var res = account.AuthCheckRes{}
	g_RPCHandler.Call(rpc.PasswdCheck, req, &res)

	var packet = network.NewWriter(CHECK_USR_PDATA)

	if res.Result {
		// password verified
		packet.WriteByte(0x01)
		session.Data.CharVerified = true
	} else {
		packet.WriteByte(0x00)
	}

	session.Send(packet)
}
