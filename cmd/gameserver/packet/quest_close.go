package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	questCloseFixedSize = 14                  // header + quest index + quest slot
	questCloseMinSize   = questCloseFixedSize // normal rewards have no bData payload
	questCloseSuccess   = byte(1)
	questCloseFailure   = byte(0)
)

// QuestCloseEvent handles CSC_QUESTCLSEVT (283). WorldSvr lays out bData as
// mission-item slots, followed by a reward choice and a reward destination
// slot when the quest has an item reward.
func QuestCloseEvent(session *network.Session, reader *network.Reader) {
	if reader.Size < questCloseMinSize {
		log.Warningf("[QUESTCLSEVT] invalid packet size: %d", reader.Size)
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	questID := reader.ReadUint16()
	questSlot := reader.ReadUint16()
	if questID == 0 || questSlot >= context.QuestSlotCount {
		log.Warningf("[QUESTCLSEVT] invalid quest: quest=%d slot=%d", questID, questSlot)
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	// WorldSvr treats bData as a WORD array. A final padding byte is not
	// consumed when the client sends the minimum normal-reward form.
	data := make([]uint16, (int(reader.Size)-questCloseFixedSize)/2)
	for index := range data {
		data[index] = reader.ReadUint16()
	}

	reward, ok := clientdata.FindQuestReward(questID)
	if !ok {
		log.Warningf("[QUESTCLSEVT] reward is not defined: quest=%d", questID)
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}
	hasRewardItem := reward.ItemSetID != 0

	missionItems := clientdata.QuestMissionItems(questID)
	if len(data) < len(missionItems) {
		log.Warningf("[QUESTCLSEVT] missing mission item slots: quest=%d got=%d want=%d",
			questID, len(data), len(missionItems))
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	dataIndex := len(missionItems)
	rewardChoice := uint16(0)
	rewardSlot := uint16(0)
	if hasRewardItem {
		if len(data) <= dataIndex {
			log.Warningf("[QUESTCLSEVT] missing reward choice: quest=%d", questID)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
		rewardChoice = data[dataIndex]
		dataIndex++
		if len(data) <= dataIndex {
			log.Warningf("[QUESTCLSEVT] missing reward inventory slot: quest=%d", questID)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
		rewardSlot = data[dataIndex]
		dataIndex++
	}

	// A normal reward does not consume bData. Keep accepting the client's
	// alignment word in that case, but never ignore fields for item rewards.
	if hasRewardItem && dataIndex != len(data) {
		log.Warningf("[QUESTCLSEVT] unexpected trailing data: quest=%d words=%d", questID, len(data)-dataIndex)
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char == nil || ctx.Char.Inventory == nil || ctx.ActiveQuests[questSlot].QuestID != questID {
		ctx.Mutex.Unlock()
		log.Warningf("[QUESTCLSEVT] quest is not active: quest=%d slot=%d", questID, questSlot)
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	characterID := ctx.Char.Id
	inv := ctx.Char.Inventory
	rewardItem := clientdata.QuestRewardItem{}
	rewardExpire := uint32(0)
	if hasRewardItem {
		var found bool
		rewardItem, found = reward.ItemForChoice(rewardChoice, ctx.Char.Style.BattleStyle)
		if !found {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] reward choice is not defined: quest=%d choice=%d style=%d",
				questID, rewardChoice, ctx.Char.Style.BattleStyle)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
		var validDuration bool
		rewardExpire, validDuration = clientdata.CashItemExpiration(rewardItem.DurationID)
		if !validDuration {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] invalid reward duration: quest=%d duration=%d",
				questID, rewardItem.DurationID)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
	}
	usedSlots := make(map[uint16]struct{}, len(missionItems)+1)
	for index, requirement := range missionItems {
		slot := data[index]
		if slot >= cashInventorySlotCount {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] invalid mission item slot: quest=%d slot=%d", questID, slot)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
		if _, exists := usedSlots[slot]; exists {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] duplicate inventory slot: quest=%d slot=%d", questID, slot)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
		usedSlots[slot] = struct{}{}

		item := inv.Get(slot)
		if !questMissionItemMatches(item, requirement) {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] mission item mismatch: quest=%d slot=%d got kind=%d opt=%d want kind=%d opt=%d",
				questID, slot, item.Kind, item.Option, requirement.Kind, requirement.Option)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
	}

	if hasRewardItem {
		if rewardSlot >= cashInventorySlotCount {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] invalid reward inventory slot: quest=%d slot=%d", questID, rewardSlot)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
		if _, exists := usedSlots[rewardSlot]; exists || inv.Get(rewardSlot).Kind != 0 {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] reward inventory slot is unavailable: quest=%d slot=%d", questID, rewardSlot)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
	}

	for _, slot := range data[:len(missionItems)] {
		if ok, err := inv.Remove(slot); err != nil || !ok {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] unable to consume mission item: quest=%d slot=%d err=%v", questID, slot, err)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
	}

	if hasRewardItem {
		item := inventory.Item{
			Kind:   rewardItem.Kind,
			Option: rewardItem.Option,
			Slot:   rewardSlot,
			Expire: rewardExpire,
		}
		if ok, err := inv.Set(rewardSlot, item); err != nil || !ok {
			ctx.Mutex.Unlock()
			log.Warningf("[QUESTCLSEVT] unable to grant reward item: quest=%d kind=%d slot=%d err=%v",
				questID, rewardItem.Kind, rewardSlot, err)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
	}
	ctx.Mutex.Unlock()

	var (
		previousLevel    uint16
		currentLevel     uint16
		experienceUpdate bool
		classRankUp      bool
	)
	if reward.Experience > 0 {
		previousLevel, currentLevel, experienceUpdate, classRankUp = addExperience(ctx, reward.Experience)
		if !experienceUpdate {
			log.Warningf("[QUESTCLSEVT] unable to grant experience: character=%d quest=%d exp=%d",
				characterID, questID, reward.Experience)
			sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
			return
		}
	}

	request := character.CloseQuestReq{
		Server:    byte(g_ServerSettings.ServerId),
		Character: characterID,
		Quest: character.ActiveQuest{
			QuestID: questID,
			Slot:    byte(questSlot),
		},
		Alz: reward.Alz,
	}
	response := character.CloseQuestRes{}
	if err := g_RPCHandler.Call(rpc.CloseQuest, &request, &response); err != nil || !response.Result {
		if err != nil {
			log.Errorf("[QUESTCLSEVT] unable to close quest: character=%d quest=%d: %s", characterID, questID, err)
		} else {
			log.Warningf("[QUESTCLSEVT] database rejected quest close: character=%d quest=%d slot=%d",
				characterID, questID, questSlot)
		}
		sendQuestCloseResult(session, questCloseFailure, 0, 0, 0)
		return
	}

	ctx.Mutex.Lock()
	if ctx.Char != nil && ctx.Char.Id == characterID && ctx.ActiveQuests[questSlot].QuestID == questID {
		ctx.ActiveQuests[questSlot] = context.ActiveQuest{}
		ctx.Char.Alz += reward.Alz
	}
	ctx.Mutex.Unlock()

	if experienceUpdate {
		ctx.Mutex.RLock()
		currentHP := ctx.Char.CurrentHP
		currentMP := ctx.Char.CurrentMP
		experience := ctx.Char.Exp
		ctx.Mutex.RUnlock()

		sendExperienceUpdate(session, experience)
		if currentLevel > previousLevel {
			sendLevelUpdate(session, currentLevel)
			sendHealthUpdate(session, currentHP)
			sendManaUpdate(session, currentMP)
			sendLevelUpEvent(session, characterID)
		}
		if classRankUp {
			sendClassRankUpdate(session, character.ClassRankForLevel(currentLevel))
			sendClassRankUpEvent(session, characterID)
		}
	}

	log.Debugf("[QUESTCLSEVT] completed: character=%d quest=%d slot=%d rewardKind=%d rewardSlot=%d exp=%d alz=%d",
		characterID, questID, questSlot, rewardItem.Kind, rewardSlot, reward.Experience, reward.Alz)
	sendQuestCloseResult(session, questCloseSuccess, rewardSlot, rewardExpire, questRewardExperience(reward.Experience))
}

func questMissionItemMatches(item inventory.Item, requirement clientdata.QuestMissionItem) bool {
	return item.Kind != 0 && item.Kind == requirement.Kind && item.Option == requirement.Option
}

func questRewardExperience(experience uint64) int32 {
	if experience >= 1<<31 {
		return 1<<31 - 1
	}
	return int32(experience)
}

func sendQuestCloseResult(session *network.Session, result byte, slot uint16, duration uint32, experience int32) {
	pkt := network.NewWriter(QUEST_CLOSE)
	pkt.WriteByte(result)
	pkt.WriteUint16(slot)
	pkt.WriteUint32(duration)
	pkt.WriteInt32(experience)
	session.Send(pkt)
}
