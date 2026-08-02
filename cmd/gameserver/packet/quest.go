package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	questOpenPacketSize   = 15 // header + quest index + slot + transmuter slot
	questUIPacketSize     = 16 // header + quest index + slot + QuestUIInfo
	questNoTransmuterSlot = uint16(0xFFFF)
	questOpenSuccess      = byte(1)
	questOpenFailure      = byte(0)
)

// QuestOpenEvent opens a quest in one of the character's active quest slots.
//
// The observed EP6 client sends the no-item/no-skill form, which consists of
// the three fields below and has no trailing bData payload. The item/skill
// opening variants are deliberately rejected until their client data and
// inventory/skill consumption rules are mapped.
func QuestOpenEvent(session *network.Session, reader *network.Reader) {
	if reader.Size < questOpenPacketSize {
		log.Warningf("[QUESTOPNEVT] invalid packet size: %d", reader.Size)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	if reader.Size != questOpenPacketSize {
		log.Warningf("[QUESTOPNEVT] unsupported opening payload: packet=%d expected=%d", reader.Size, questOpenPacketSize)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	questID := reader.ReadUint16()
	questSlot := reader.ReadByte()
	transmuterSlot := reader.ReadUint16()

	if questID == 0 || questSlot >= context.QuestSlotCount {
		log.Warningf("[QUESTOPNEVT] invalid quest: quest=%d slot=%d", questID, questSlot)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	if transmuterSlot != questNoTransmuterSlot {
		log.Warningf("[QUESTOPNEVT] transmuter quest opening is not implemented: quest=%d slot=%d transmuter=%d", questID, questSlot, transmuterSlot)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char == nil {
		ctx.Mutex.Unlock()
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	if ctx.ActiveQuests[questSlot].QuestID != 0 {
		log.Warningf("[QUESTOPNEVT] quest slot is occupied: character=%d quest=%d slot=%d", ctx.Char.Id, questID, questSlot)
		ctx.Mutex.Unlock()
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	for slot, active := range ctx.ActiveQuests {
		if active.QuestID == questID {
			log.Warningf("[QUESTOPNEVT] quest is already active: character=%d quest=%d slot=%d", ctx.Char.Id, questID, slot)
			ctx.Mutex.Unlock()
			sendQuestOpenResult(session, questOpenFailure)
			return
		}
	}

	characterID := ctx.Char.Id
	ctx.Mutex.Unlock()

	request := character.OpenQuestReq{
		Server:    byte(g_ServerSettings.ServerId),
		Character: characterID,
		Quest: character.ActiveQuest{
			QuestID:        questID,
			Slot:           questSlot,
			TransmuterSlot: transmuterSlot,
		},
	}
	response := character.OpenQuestRes{}
	if err := g_RPCHandler.Call(rpc.OpenQuest, &request, &response); err != nil {
		log.Errorf("[QUESTOPNEVT] unable to save quest: character=%d quest=%d: %s", characterID, questID, err)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}
	if !response.Result {
		log.Warningf("[QUESTOPNEVT] quest was rejected by database: character=%d quest=%d slot=%d", characterID, questID, questSlot)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char == nil || ctx.Char.Id != characterID || ctx.ActiveQuests[questSlot].QuestID != 0 {
		ctx.Mutex.Unlock()
		log.Warningf("[QUESTOPNEVT] runtime changed while saving quest: character=%d quest=%d slot=%d", characterID, questID, questSlot)
		sendQuestOpenResult(session, questOpenFailure)
		return
	}

	ctx.ActiveQuests[questSlot] = context.ActiveQuest{
		QuestID:        questID,
		Slot:           questSlot,
		TransmuterSlot: transmuterSlot,
	}

	log.Debugf("[QUESTOPNEVT] opened quest: character=%d quest=%d slot=%d", ctx.Char.Id, questID, questSlot)
	ctx.Mutex.Unlock()
	sendQuestOpenResult(session, questOpenSuccess)
}

func sendQuestOpenResult(session *network.Session, result byte) {
	pkt := network.NewWriter(QUEST_OPEN)
	pkt.WriteByte(result)
	session.Send(pkt)
}

// QuestUIInfo updates the display flags of an already active quest. WorldSvr
// handles this as a client-only state change and sends no response packet.
func QuestUIInfo(session *network.Session, reader *network.Reader) {
	if reader.Size != questUIPacketSize {
		log.Warningf("[QUEST_UI_INFO] invalid packet size: %d", reader.Size)
		return
	}

	questID := reader.ReadUint16()
	slotIndex := reader.ReadUint16()
	showDesc := reader.ReadByte()
	expand := reader.ReadByte()

	if questID == 0 || slotIndex >= context.QuestSlotCount {
		log.Warningf("[QUEST_UI_INFO] invalid quest: quest=%d slot=%d", questID, slotIndex)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	slot := byte(slotIndex)
	ctx.Mutex.Lock()
	if ctx.Char == nil || ctx.ActiveQuests[slot].QuestID != questID {
		ctx.Mutex.Unlock()
		log.Warningf("[QUEST_UI_INFO] quest is not active: quest=%d slot=%d", questID, slot)
		return
	}
	characterID := ctx.Char.Id
	ctx.Mutex.Unlock()

	request := character.QuestUIRequest{
		Server:    byte(g_ServerSettings.ServerId),
		Character: characterID,
		Quest: character.ActiveQuest{
			QuestID:  questID,
			Slot:     slot,
			ShowDesc: showDesc,
			Expand:   expand,
		},
	}
	response := character.QuestUIResponse{}
	if err := g_RPCHandler.Call(rpc.SaveQuestUI, &request, &response); err != nil {
		log.Errorf("[QUEST_UI_INFO] unable to save UI state: character=%d quest=%d: %s", characterID, questID, err)
		return
	}
	if !response.Result {
		log.Warningf("[QUEST_UI_INFO] database rejected UI state: character=%d quest=%d slot=%d", characterID, questID, slot)
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char != nil && ctx.Char.Id == characterID && ctx.ActiveQuests[slot].QuestID == questID {
		ctx.ActiveQuests[slot].ShowDesc = showDesc
		ctx.ActiveQuests[slot].Expand = expand
	}
	ctx.Mutex.Unlock()

	log.Debugf("[QUEST_UI_INFO] saved: character=%d quest=%d slot=%d showDesc=%d expand=%d", characterID, questID, slot, showDesc, expand)
}
