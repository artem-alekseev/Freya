package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	comboResultOK          byte = 0
	comboResultNotEnoughSP byte = 1
	comboResultNotCoolTime byte = 2
	comboStyleExMask       byte = 0x80
)

// RequestComboSkillEvent handles REQ_COMBSKEVENT. The client sends the slot
// of the combo-start skill; the server answers with COMBSKILSET and then
// broadcasts the complete styleEx through NFY_COMBSKEVENT.
func RequestComboSkillEvent(session *network.Session, reader *network.Reader) {
	const packetSize = 11 // packet header + one-byte skill slot

	if reader.Size != packetSize {
		log.Warningf("[REQ_COMBSKEVENT] invalid packet size: %d", reader.Size)
		return
	}

	slot := uint16(reader.ReadByte())
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	var (
		result    = comboResultOK
		currentMP uint16
		currentSP uint16
		charID    int32
		styleEx   byte
		started   bool
		save      bool
		world     context.WorldHandler
	)

	ctx.Mutex.Lock()
	if ctx.Char == nil {
		ctx.Mutex.Unlock()
		return
	}

	char := ctx.Char
	comboSkill, validSlot := comboSkillForStyle(char.Style.BattleStyle, slot)
	learned := char.Skills.Get(slot)
	if !validSlot || learned.Id != comboSkill.SkillID || learned.Level == 0 {
		log.Warningf("[REQ_COMBSKEVENT] invalid combo skill: character=%d slot=%d skill=%d",
			char.Id, slot, learned.Id)
		ctx.Mutex.Unlock()
		return
	}

	meta, hasMeta := clientdata.FindSkillMeta(comboSkill.SkillID)
	if !hasMeta {
		log.Warningf("[REQ_COMBSKEVENT] missing skill metadata: character=%d skill=%d",
			char.Id, comboSkill.SkillID)
		ctx.Mutex.Unlock()
		return
	}

	if !ctx.ComboActive {
		if int(char.CurrentSP) < meta.SPWaste {
			result = comboResultNotEnoughSP
		} else {
			mpWaste := meta.MPWaste(learned.Level)
			if char.CurrentMP < mpWaste {
				char.CurrentMP = 0
			} else {
				char.CurrentMP -= mpWaste
			}
			char.CurrentSP -= uint16(meta.SPWaste)
			ctx.ComboActive = true
			started = true
			save = true
		}
	} else {
		ctx.ComboActive = false
	}

	currentMP = char.CurrentMP
	currentSP = char.CurrentSP
	charID = char.Id
	styleEx = ctx.BattleMode.StyleEx()
	if ctx.ComboActive {
		styleEx |= comboStyleExMask
	}
	world = ctx.World
	ctx.Mutex.Unlock()

	sendComboSkillSet(session, result, currentMP)
	if result != comboResultOK {
		return
	}

	if save {
		saveContextVitals(ctx)
	}

	if world != nil {
		pkt := network.NewWriter(NFY_COMBSKEVENT)
		pkt.WriteInt32(charID)
		pkt.WriteByte(styleEx)
		world.BroadcastSessionPacket(session, pkt)
	}

	if started {
		sendSpiritUpdate(session, currentSP)
	}
}

func comboSkillForStyle(style byte, slot uint16) (clientdata.BattleModeSkill, bool) {
	for _, skill := range clientdata.FindBattleModeSkills(style, 1) {
		if skill.Slot == slot {
			return skill, true
		}
	}
	return clientdata.BattleModeSkill{}, false
}

func sendComboSkillSet(session *network.Session, result byte, currentMP uint16) {
	pkt := network.NewWriter(COMBSKILSET)
	pkt.WriteByte(result)
	pkt.WriteUint16(currentMP)
	session.Send(pkt)
}
