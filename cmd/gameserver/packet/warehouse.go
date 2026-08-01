package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

// GetMyWarehs handles CSC_GETMYWAREHS and returns the character's warehouse
// in the S2C_GETMYWAREHS layout expected by the client.
func GetMyWarehs(session *network.Session, reader *network.Reader) {
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	warehouse := &ctx.Warehouse
	ctx.Mutex.RUnlock()

	data, count := warehouse.Serialize()
	pkt := network.NewWriter(GETMYWAREHS)
	pkt.WriteUint16(uint16(count)) // wWareHNum
	pkt.WriteUint64(0)             // Alz
	pkt.WriteBytes(data)

	session.Send(pkt)
}
