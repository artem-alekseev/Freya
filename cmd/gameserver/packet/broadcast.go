package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	broadcastHeaderSize = 10
	broadcastTypeSize   = 4
	broadcastLengthSize = 2
	broadcastChannel    = 1
)

// RequestBroadcast handles REQ_BROADCAST. The data block contains the
// client's message/link representation and must be forwarded byte-for-byte.
func RequestBroadcast(session *network.Session, reader *network.Reader) {
	if reader.Size < broadcastHeaderSize+broadcastTypeSize+broadcastLengthSize {
		log.Warning("[REQ_BROADCAST] packet is too short")
		return
	}

	broadcastType := reader.ReadInt32()
	dataLen := reader.ReadUint16()
	remaining := int(reader.Size) - broadcastHeaderSize - broadcastTypeSize - broadcastLengthSize
	if int(dataLen) > remaining {
		log.Warningf("[REQ_BROADCAST] invalid data length %d, remaining %d", dataLen, remaining)
		return
	}
	if broadcastType != broadcastChannel {
		log.Warningf("[REQ_BROADCAST] unsupported broadcast type %d", broadcastType)
		return
	}

	data := reader.ReadBytes(int(dataLen))
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error("[REQ_BROADCAST]", err)
		return
	}

	ctx.Mutex.RLock()
	characterID := ctx.Char.Id
	world := ctx.World
	worldManager := ctx.WorldManager
	ctx.Mutex.RUnlock()

	pkt := network.NewWriter(NFY_BROADCAST)
	pkt.WriteInt32(broadcastType)
	pkt.WriteInt32(characterID)
	pkt.WriteUint16(dataLen)
	pkt.WriteBytes(data)

	if worldManager != nil {
		worldManager.BroadcastAllPacket(pkt)
		return
	}
	if world != nil {
		world.BroadcastAllPacket(pkt)
		return
	}

	log.Error("[REQ_BROADCAST] unable to get world manager")
}
