package packet

import (
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const disconnectRequestPacketSize = 10 // RES_DISCONNECTS: C2S header only

// RequestDisconnects handles the client's normal GameServer disconnect request.
// The packet has no payload; the server closes the session after validating its
// complete size. Socket cleanup and character persistence are handled by the
// regular ClientDisconnectEvent path.
func RequestDisconnects(session *network.Session, reader *network.Reader) {
	if reader.Size != disconnectRequestPacketSize {
		log.Warningf("[REQ_DISCONNECTS] invalid packet size: %d", reader.Size)
		return
	}

	session.Close()
}
