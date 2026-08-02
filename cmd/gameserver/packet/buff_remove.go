package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	buffRemoveBlessSelfPacketSize = 12

	buffRemoveResultOK        byte = 0
	buffRemoveResultUndefined byte = 1
	buffRemoveResultNotExist  byte = 2

	buffTypeBless uint16 = 0
	buffKindBless byte   = 0
)

// BuffRemoveBlessSelf handles WorldSvr's CSC_BUFFREMOVEBLESSSELF packet.
func BuffRemoveBlessSelf(session *network.Session, reader *network.Reader) {
	if reader.Size != buffRemoveBlessSelfPacketSize {
		log.Warningf("[BUFFREMOVEBLESSSELF] invalid packet size: %d", reader.Size)
		return
	}

	skillID := reader.ReadUint16()
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	removed, remaining := ctx.RemoveBuff(skillID, buffTypeBless, buffKindBless)
	result := buffRemoveResultUndefined
	if removed {
		result = buffRemoveResultOK
	} else {
		result = buffRemoveResultNotExist
	}

	reply := network.NewWriter(BUFFREMOVEBLESSSELF)
	reply.WriteUint16(skillID)
	reply.WriteByte(result)
	session.Send(reply)

	if !removed {
		log.Debugf("[BUFFREMOVEBLESSSELF] buff not found: character=%d skill=%d",
			characterID(ctx), skillID)
		return
	}

	characterIDValue := characterID(ctx)
	ctx.Mutex.RLock()
	world := ctx.World
	ctx.Mutex.RUnlock()
	if world == nil {
		return
	}

	remainingBlesses := make([]context.ActiveBuff, 0, len(remaining))
	for _, buff := range remaining {
		if buff.BuffType == buffTypeBless && buff.BuffKind == buffKindBless {
			remainingBlesses = append(remainingBlesses, buff)
		}
	}

	notification := network.NewWriter(NFY_STOPBUFFING)
	notification.WriteInt32(characterIDValue)
	notification.WriteByte(byte(buffTypeBless))
	notification.WriteByte(byte(len(remainingBlesses)))
	for _, buff := range remainingBlesses {
		notification.WriteUint16(buff.SkillID)
		notification.WriteUint16(uint16(buff.Level))
	}
	world.BroadcastSessionPacket(session, notification)

	log.Debugf("[BUFFREMOVEBLESSSELF] removed: character=%d skill=%d remaining=%d",
		characterIDValue, skillID, len(remainingBlesses))
}
