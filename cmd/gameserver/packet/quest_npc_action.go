package packet

import (
	"fmt"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	questNPCActionBaseSize = 18 // header + quest + set + quest slot + action count
	questNPCActionUnitSize = 6  // action index + inventory slot

	questActionGive byte = 0
	questActionTake byte = 1
	questActionTalk byte = 2
)

// QuestNPCAction handles CSC_QSTNPCACTIN (320). The packet is variable-length:
// the first action is followed by zero or more NPCACT_UNIT records. The
// current client sends a destination inventory slot for QACT_GIVE and the
// source inventory slot for QACT_TAKE.
func QuestNPCAction(session *network.Session, reader *network.Reader) {
	if reader.Size < questNPCActionBaseSize ||
		(int(reader.Size)-questNPCActionBaseSize)%questNPCActionUnitSize != 0 {
		log.Warningf("[QSTNPCACTIN] invalid packet size: %d", reader.Size)
		return
	}

	questID := reader.ReadUint16()
	setID := reader.ReadInt32()
	questSlot := reader.ReadByte()
	actionCount := reader.ReadByte()
	if questID == 0 || questSlot >= context.QuestSlotCount || actionCount == 0 {
		log.Warningf("[QSTNPCACTIN] invalid request: quest=%d set=%d slot=%d actions=%d",
			questID, setID, questSlot, actionCount)
		return
	}

	actions := make([]questNPCActionRequest, int(actionCount))
	for index := range actions {
		actions[index] = questNPCActionRequest{
			Index:         reader.ReadInt32(),
			InventorySlot: reader.ReadUint16(),
		}
	}

	definitions := make([]clientdata.QuestNPCAction, len(actions))
	for index, request := range actions {
		definition, ok := clientdata.FindQuestNPCAction(questID, setID, request.Index)
		if !ok {
			log.Warningf("[QSTNPCACTIN] unknown action: quest=%d set=%d index=%d",
				questID, setID, request.Index)
			return
		}
		if definition.Order <= 0 || definition.Order >= 16 {
			log.Warningf("[QSTNPCACTIN] invalid action order: quest=%d set=%d index=%d order=%d",
				questID, setID, request.Index, definition.Order)
			return
		}
		if index > 0 && definition.Order != definitions[0].Order {
			log.Warningf("[QSTNPCACTIN] actions belong to different NPC orders: quest=%d set=%d",
				questID, setID)
			return
		}
		if definition.ActionType != questActionGive &&
			definition.ActionType != questActionTake &&
			definition.ActionType != questActionTalk {
			log.Warningf("[QSTNPCACTIN] unsupported action type: quest=%d set=%d index=%d type=%d",
				questID, setID, request.Index, definition.ActionType)
			return
		}
		if request.InventorySlot >= normalInventorySlotCount+tempInventorySlotCount {
			log.Warningf("[QSTNPCACTIN] invalid inventory slot: quest=%d slot=%d",
				questID, request.InventorySlot)
			return
		}
		definitions[index] = definition
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char == nil || ctx.Char.Inventory == nil {
		ctx.Mutex.Unlock()
		log.Warningf("[QSTNPCACTIN] character data is not initialized: quest=%d", questID)
		return
	}
	activeQuest := &ctx.ActiveQuests[questSlot]
	if activeQuest.QuestID != questID {
		ctx.Mutex.Unlock()
		log.Warningf("[QSTNPCACTIN] quest is not active: character=%d quest=%d slot=%d",
			ctx.Char.Id, questID, questSlot)
		return
	}

	npcFlag := uint16(1 << uint(definitions[0].Order))
	if activeQuest.NPCFlags&npcFlag != 0 {
		ctx.Mutex.Unlock()
		log.Warningf("[QSTNPCACTIN] action is already completed: character=%d quest=%d order=%d",
			ctx.Char.Id, questID, definitions[0].Order)
		return
	}

	if err := validateQuestNPCInventoryActions(ctx.Char.Inventory, actions, definitions); err != nil {
		ctx.Mutex.Unlock()
		log.Warningf("[QSTNPCACTIN] rejected: character=%d quest=%d: %s", ctx.Char.Id, questID, err)
		return
	}

	for index, definition := range definitions {
		request := actions[index]
		switch definition.ActionType {
		case questActionGive:
			item := inventoryItemFromQuestAction(request.InventorySlot, definition)
			ok, err := ctx.Char.Inventory.Set(request.InventorySlot, item)
			if err != nil || !ok {
				ctx.Mutex.Unlock()
				log.Warningf("[QSTNPCACTIN] unable to give item: character=%d quest=%d slot=%d kind=%d err=%v",
					ctx.Char.Id, questID, request.InventorySlot, item.Kind, err)
				return
			}
		case questActionTake:
			ok, err := ctx.Char.Inventory.Remove(request.InventorySlot)
			if err != nil || !ok {
				ctx.Mutex.Unlock()
				log.Warningf("[QSTNPCACTIN] unable to take item: character=%d quest=%d slot=%d err=%v",
					ctx.Char.Id, questID, request.InventorySlot, err)
				return
			}
		}
	}

	activeQuest.NPCFlags |= npcFlag
	flagNPC := activeQuest.NPCFlags
	expectedNPCFlags := flagNPC &^ npcFlag
	characterID := ctx.Char.Id
	ctx.Mutex.Unlock()

	request := character.SaveQuestNPCFlagsReq{
		Server:           byte(g_ServerSettings.ServerId),
		Character:        characterID,
		ExpectedNPCFlags: expectedNPCFlags,
		Quest: character.ActiveQuest{
			QuestID:  questID,
			Slot:     questSlot,
			NPCFlags: flagNPC,
		},
	}
	response := character.SaveQuestNPCFlagsRes{}
	if err := g_RPCHandler.Call(rpc.SaveQuestNPCFlags, &request, &response); err != nil {
		log.Errorf("[QSTNPCACTIN] unable to save stage: character=%d quest=%d order=%d: %s",
			characterID, questID, definitions[0].Order, err)
		return
	}
	if !response.Result {
		log.Warningf("[QSTNPCACTIN] database rejected stage: character=%d quest=%d order=%d flags=%d",
			characterID, questID, definitions[0].Order, flagNPC)
		return
	}

	log.Debugf("[QSTNPCACTIN] completed: character=%d quest=%d set=%d order=%d actions=%d flags=%d",
		characterID, questID, setID, definitions[0].Order, len(actions), flagNPC)
	sendQuestNPCActionResult(session, questID, flagNPC, setID)
}

type questNPCActionRequest struct {
	Index         int32
	InventorySlot uint16
}

func validateQuestNPCInventoryActions(inv *inventory.Inventory, requests []questNPCActionRequest, definitions []clientdata.QuestNPCAction) error {
	usedSlots := make(map[uint16]struct{}, len(requests))
	for index, definition := range definitions {
		request := requests[index]
		if _, exists := usedSlots[request.InventorySlot]; exists {
			return fmt.Errorf("inventory slot %d is used more than once", request.InventorySlot)
		}
		usedSlots[request.InventorySlot] = struct{}{}

		item := inv.Get(request.InventorySlot)
		switch definition.ActionType {
		case questActionGive:
			if definition.Values[0] <= 0 {
				return fmt.Errorf("quest action has invalid item kind %d", definition.Values[0])
			}
			if item.Kind != 0 {
				return fmt.Errorf("inventory slot %d is occupied", request.InventorySlot)
			}
		case questActionTake:
			if item.Kind == 0 {
				return fmt.Errorf("inventory slot %d is empty", request.InventorySlot)
			}
			if item.Kind != uint32(definition.Values[0]) || item.Option != definition.Values[1] {
				return fmt.Errorf("inventory item mismatch in slot %d: got kind=%d opt=%d, want kind=%d opt=%d",
					request.InventorySlot, item.Kind, item.Option, definition.Values[0], definition.Values[1])
			}
		}
	}
	return nil
}

func inventoryItemFromQuestAction(slot uint16, definition clientdata.QuestNPCAction) inventory.Item {
	return inventory.Item{Kind: uint32(definition.Values[0]), Option: definition.Values[1], Slot: slot}
}

func sendQuestNPCActionResult(session *network.Session, questID, flagNPC uint16, setID int32) {
	pkt := network.NewWriter(QUEST_NPC_ACTION)
	pkt.WriteUint16(questID)
	pkt.WriteUint16(flagNPC)
	pkt.WriteInt32(setID)
	session.Send(pkt)
}
