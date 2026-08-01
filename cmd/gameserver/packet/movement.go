package packet

import (
	"math"
	"time"

	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

// MoveBegined Packet
func MoveBegined(session *network.Session, reader *network.Reader) {
	startX := reader.ReadInt16()
	startY := reader.ReadInt16()
	endX := reader.ReadInt16()
	endY := reader.ReadInt16()
	_ = reader.ReadInt16() // pnt x
	_ = reader.ReadInt16() // pnt y
	_ = reader.ReadInt16() // world map

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	if ctx.World == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_MOVEBEGINED)
	pkt.WriteInt32(id)
	pkt.WriteUint32(time.Now().Unix()) // move begin time
	pkt.WriteInt16(startX)
	pkt.WriteInt16(startY)
	pkt.WriteInt16(endX)
	pkt.WriteInt16(endY)

	ctx.Mutex.Lock()
	ctx.Char.BeginX = startX
	ctx.Char.BeginY = startY
	ctx.Char.EndX = endX
	ctx.Char.EndY = endY
	ctx.Mutex.Unlock()

	ctx.World.BroadcastSessionPacket(session, pkt)
}

// MoveEnded Packet
func MoveEnded(session *network.Session, reader *network.Reader) {
	pntX := reader.ReadInt16()
	pntY := reader.ReadInt16()

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_MOVEENDED00)
	pkt.WriteInt32(id)
	pkt.WriteInt16(pntX)
	pkt.WriteInt16(pntY)

	ctx.Mutex.Lock()
	ctx.Char.BeginX = pntX
	ctx.Char.BeginY = pntY
	ctx.Char.EndX = pntX
	ctx.Char.EndY = pntY

	ctx.Char.X = byte(pntX)
	ctx.Char.Y = byte(pntY)
	ctx.Mutex.Unlock()

	world.BroadcastSessionPacket(session, pkt)
}

// MoveChanged Packet
func MoveChanged(session *network.Session, reader *network.Reader) {
	startX := reader.ReadInt16()
	startY := reader.ReadInt16()
	endX := reader.ReadInt16()
	endY := reader.ReadInt16()
	_ = reader.ReadInt16() // pnt x
	_ = reader.ReadInt16() // pnt y
	_ = reader.ReadInt16() // world map

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_MOVECHANGED)
	pkt.WriteInt32(id)
	pkt.WriteUint32(time.Now().Unix()) // move begin time
	pkt.WriteInt16(startX)
	pkt.WriteInt16(startY)
	pkt.WriteInt16(endX)
	pkt.WriteInt16(endY)

	ctx.Mutex.Lock()
	ctx.Char.BeginX = startX
	ctx.Char.BeginY = startY
	ctx.Char.EndX = endX
	ctx.Char.EndY = endY
	ctx.Mutex.Unlock()

	world.BroadcastSessionPacket(session, pkt)
}

// MoveTilePos packet
func MoveTilePos(session *network.Session, reader *network.Reader) {
	_ = reader.ReadInt16()     // curr x
	_ = reader.ReadInt16()     // curr y
	pntX := reader.ReadInt16() // pnt x
	pntY := reader.ReadInt16() // pnt y

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	ctx.Mutex.Lock()
	ctx.Char.X = byte(pntX)
	ctx.Char.Y = byte(pntY)
	ctx.Mutex.Unlock()

	world.AdjustCell(session)
}

// ChangeDirection Packet
func ChangeDirection(session *network.Session, reader *network.Reader) {
	direction := reader.ReadUint32() // float

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_CHANGEDIRECTION)
	pkt.WriteInt32(id)
	pkt.WriteUint32(direction) // float

	world.BroadcastSessionPacket(session, pkt)
}

// KeyMoveBegined Packet
func KeyMoveBegined(session *network.Session, reader *network.Reader) {
	startX := reader.ReadUint32() // float
	startY := reader.ReadUint32() // float
	endX := reader.ReadUint32()   // float
	endY := reader.ReadUint32()   // float
	_ = reader.ReadUint32()       // pnt x
	_ = reader.ReadUint32()       // pnt y
	dir := reader.ReadByte()

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_KEYMOVEBEGINED)
	pkt.WriteInt32(id)
	pkt.WriteUint32(time.Now().Unix()) // move begin time
	pkt.WriteUint32(startX)
	pkt.WriteUint32(startY)
	pkt.WriteUint32(endX)
	pkt.WriteUint32(endY)
	pkt.WriteByte(dir)

	world.BroadcastSessionPacket(session, pkt)
}

// KeyMoveEnded Packet
func KeyMoveEnded(session *network.Session, reader *network.Reader) {
	pntXBits := reader.ReadUint32()
	pntYBits := reader.ReadUint32()
	pntX := math.Float32frombits(pntXBits)
	pntY := math.Float32frombits(pntYBits)

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_KEYMOVEENDED00)
	pkt.WriteInt32(id)
	pkt.WriteUint32(pntXBits)
	pkt.WriteUint32(pntYBits)

	if !math.IsNaN(float64(pntX)) && !math.IsNaN(float64(pntY)) &&
		!math.IsInf(float64(pntX), 0) && !math.IsInf(float64(pntY), 0) &&
		pntX >= 0 && pntX <= 255 && pntY >= 0 && pntY <= 255 {
		ctx, err := context.Parse(session)
		if err == nil {
			ctx.Mutex.Lock()
			ctx.Char.BeginX = int16(pntX)
			ctx.Char.BeginY = int16(pntY)
			ctx.Char.EndX = int16(pntX)
			ctx.Char.EndY = int16(pntY)
			ctx.Char.X = byte(pntX)
			ctx.Char.Y = byte(pntY)
			ctx.Mutex.Unlock()
			world.AdjustCell(session)
		}
	}

	world.BroadcastSessionPacket(session, pkt)
}

// KeyMoveChanged Packet
func KeyMoveChanged(session *network.Session, reader *network.Reader) {
	startX := reader.ReadUint32() // float
	startY := reader.ReadUint32() // float
	endX := reader.ReadUint32()   // float
	endY := reader.ReadUint32()   // float
	_ = reader.ReadUint32()       // pnt x
	_ = reader.ReadUint32()       // pnt y
	dir := reader.ReadByte()

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(NFY_KEYMOVECHANGED)
	pkt.WriteInt32(id)
	pkt.WriteUint32(time.Now().Unix()) // move begin time
	pkt.WriteUint32(startX)
	pkt.WriteUint32(startY)
	pkt.WriteUint32(endX)
	pkt.WriteUint32(endY)
	pkt.WriteByte(dir)

	world.BroadcastSessionPacket(session, pkt)
}
