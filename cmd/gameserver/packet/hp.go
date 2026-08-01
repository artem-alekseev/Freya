package packet

import (
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	updateTypeHP   byte = 3
	updateTypeMP   byte = 4
	updateTypeRank byte = 9
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

// sendManaUpdate follows the client's NFY_UPDATEDATAS layout for UT_MP,
// whose value is stored in the union's integer field.
func sendManaUpdate(session *network.Session, currentMP uint16) {
	sendUpdatedData(session, updateTypeMP, uint64(currentMP))
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
}
