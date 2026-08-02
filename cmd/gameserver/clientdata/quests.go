package clientdata

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxQuestMissionCounters = 3
const questRewardValueCount = 12

var questMissionCounts = make(map[uint16]byte)
var questMissionItems = make(map[uint16][]QuestMissionItem)
var questRewards = make(map[uint16]QuestReward)

// QuestMissionItem is the item requirement of a type-1 mission in quest.dec.
// The client sends its inventory slot before the reward selection in the
// QUESTCLSEVT payload.
type QuestMissionItem struct {
	Kind   uint32
	Option int32
	Count  uint32
}

// QuestRewardItem is one choice from a reward_item_set in quest.dec.
type QuestRewardItem struct {
	Kind       uint32
	Option     int32
	DurationID byte
	Class      byte
	Order      uint16
}

// QuestReward contains the default reward fields used by QUESTCLSEVT.
type QuestReward struct {
	QuestID         uint16
	Experience      uint64
	Alz             uint64
	ItemSetID       uint16
	Items           []QuestRewardItem
	SkillExperience uint64
}

// ItemForChoice resolves a reward choice for the character's battle style.
// Class 0 is the common pool; class-specific entries take precedence when
// present. The client data already stores the final item option and duration.
func (reward QuestReward) ItemForChoice(choice uint16, battleStyle byte) (QuestRewardItem, bool) {
	classSpecific := make([]QuestRewardItem, 0, len(reward.Items))
	common := make([]QuestRewardItem, 0, len(reward.Items))
	for _, item := range reward.Items {
		if item.Class == battleStyle && item.Class != 0 {
			classSpecific = append(classSpecific, item)
		}
		if item.Class == 0 {
			common = append(common, item)
		}
	}
	items := common
	if len(classSpecific) > 0 {
		items = classSpecific
	}
	if int(choice) >= len(items) {
		return QuestRewardItem{}, false
	}
	return items[choice], true
}

// QuestNPCAction contains the server-relevant part of one <quest_npc>
// definition from quest.dec. Index is the value sent by the client in
// NPCACT_UNIT.iActIdx, while Order is the NPC progress bit used by WorldSvr.
type QuestNPCAction struct {
	QuestID    uint16
	SetID      uint16
	Index      int32
	Order      int32
	ActionType byte
	Values     [3]int32
}

type questNPCActionKey struct {
	questID uint16
	setID   uint16
	index   int32
}

var questNPCActions = make(map[questNPCActionKey]QuestNPCAction)

// QuestMissionCount returns the number of counter bytes expected by the
// client's QUESTLIST_DATA0 record for a quest.
func QuestMissionCount(questID uint16) byte {
	return questMissionCounts[questID]
}

// QuestMissionItems returns a copy of the item requirements for a quest.
func QuestMissionItems(questID uint16) []QuestMissionItem {
	items := questMissionItems[questID]
	if len(items) == 0 {
		return nil
	}
	return append([]QuestMissionItem(nil), items...)
}

// FindQuestReward returns the reward vector parsed from quest.dec.
func FindQuestReward(questID uint16) (QuestReward, bool) {
	reward, ok := questRewards[questID]
	return reward, ok
}

// FindQuestNPCAction returns the client-data definition addressed by a
// CSC_QSTNPCACTIN action index.
func FindQuestNPCAction(questID uint16, setID int32, index int32) (QuestNPCAction, bool) {
	if setID < 0 || setID > 0xFFFF {
		return QuestNPCAction{}, false
	}
	action, ok := questNPCActions[questNPCActionKey{
		questID: questID,
		setID:   uint16(setID),
		index:   index,
	}]
	return action, ok
}

func parseQuestMissionCounts(data []byte) (map[uint16]byte, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	counts := make(map[uint16]byte)

	var (
		questID     uint16
		missionNum  int
		inQuestData bool
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse quest.dec: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "quest":
				// The document itself is wrapped in a root <quest> element.
				// Only nested quest definitions have a quest_id attribute.
				id, present, err := optionalUintAttribute(element, "quest_id", 16)
				if err != nil {
					return nil, err
				}
				if !present {
					continue
				}
				if id == 0 || id > 0xFFFF {
					return nil, fmt.Errorf("invalid quest id %d", id)
				}
				questID = uint16(id)
				missionNum = 0
				inQuestData = true
			case "mission":
				if !inQuestData {
					continue
				}
				missionNum++
				if missionNum > maxQuestMissionCounters {
					return nil, fmt.Errorf("quest %d has more than %d mission counters", questID, maxQuestMissionCounters)
				}
			}
		case xml.EndElement:
			if element.Name.Local != "quest" || !inQuestData {
				continue
			}
			if _, exists := counts[questID]; exists {
				return nil, fmt.Errorf("duplicate quest definition %d", questID)
			}
			counts[questID] = byte(missionNum)
			inQuestData = false
		}
	}

	if len(counts) == 0 {
		return nil, fmt.Errorf("quest.dec does not contain quest definitions")
	}
	return counts, nil
}

func parseQuestMissionItems(data []byte) (map[uint16][]QuestMissionItem, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	items := make(map[uint16][]QuestMissionItem)

	var (
		questID     uint16
		inQuestData bool
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse quest mission items: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "quest":
				id, present, err := optionalUintAttribute(element, "quest_id", 16)
				if err != nil {
					return nil, err
				}
				if !present {
					continue
				}
				if id == 0 || id > 0xFFFF {
					return nil, fmt.Errorf("invalid quest id %d", id)
				}
				questID = uint16(id)
				inQuestData = true
			case "mission":
				if !inQuestData {
					continue
				}
				typeValue, present, err := optionalQuestIntAttribute(element, "type")
				if err != nil {
					return nil, err
				}
				if !present || typeValue != 1 {
					continue
				}

				value, present := questAttribute(element, "value")
				if !present || value == "" {
					return nil, fmt.Errorf("quest %d has an empty item mission", questID)
				}
				parts := strings.Split(value, ":")
				if len(parts) < 3 {
					return nil, fmt.Errorf("quest %d has invalid item mission value %q", questID, value)
				}
				kind, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
				if err != nil || kind == 0 {
					return nil, fmt.Errorf("quest %d has invalid item mission kind %q", questID, parts[0])
				}
				option, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 32)
				if err != nil {
					return nil, fmt.Errorf("quest %d has invalid item mission option %q", questID, parts[1])
				}
				count, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 32)
				if err != nil || count == 0 {
					return nil, fmt.Errorf("quest %d has invalid item mission count %q", questID, parts[2])
				}
				items[questID] = append(items[questID], QuestMissionItem{
					Kind:   uint32(kind),
					Option: int32(option),
					Count:  uint32(count),
				})
			}
		case xml.EndElement:
			if element.Name.Local == "quest" && inQuestData {
				inQuestData = false
			}
		}
	}

	return items, nil
}

func parseQuestRewards(data []byte) (map[uint16]QuestReward, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	rewards := make(map[uint16]QuestReward)
	rewardItemSets := make(map[uint16][]QuestRewardItem)

	var (
		questID     uint16
		inQuestData bool
		rewardSetID uint16
		inRewardSet bool
	)

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse quest rewards: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "reward_item_set":
				id, present, err := optionalQuestIntAttribute(element, "id")
				if err != nil {
					return nil, err
				}
				if !present || id <= 0 || id > 0xFFFF {
					return nil, fmt.Errorf("invalid reward item set id %d", id)
				}
				rewardSetID = uint16(id)
				inRewardSet = true
			case "reward_item":
				if !inRewardSet {
					continue
				}
				item, err := parseQuestRewardItem(element)
				if err != nil {
					return nil, err
				}
				rewardItemSets[rewardSetID] = append(rewardItemSets[rewardSetID], item)
			case "quest":
				id, present, err := optionalUintAttribute(element, "quest_id", 16)
				if err != nil {
					return nil, err
				}
				if !present {
					continue
				}
				if id == 0 || id > 0xFFFF {
					return nil, fmt.Errorf("invalid quest id %d", id)
				}
				questID = uint16(id)
				inQuestData = true
			case "param":
				if !inQuestData {
					continue
				}
				value, present := questAttribute(element, "reward")
				if !present || value == "" {
					continue
				}
				if _, exists := rewards[questID]; exists {
					return nil, fmt.Errorf("duplicate reward definition for quest %d", questID)
				}
				values, err := parseQuestRewardValues(value)
				if err != nil {
					return nil, fmt.Errorf("quest %d: %w", questID, err)
				}

				reward := QuestReward{QuestID: questID}
				if values[0] > 0 {
					reward.Experience = uint64(values[0])
				}
				if values[1] > 0 {
					reward.Alz = uint64(values[1])
				}
				if values[5] > 0 {
					if values[5] > 0xFFFF {
						return nil, fmt.Errorf("quest %d has invalid reward item set %d", questID, values[5])
					}
					reward.ItemSetID = uint16(values[5])
				}
				if values[6] > 0 {
					reward.SkillExperience = uint64(values[6])
				}
				rewards[questID] = reward
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "quest":
				if inQuestData {
					inQuestData = false
				}
			case "reward_item_set":
				inRewardSet = false
			}
		}
	}

	for questID, reward := range rewards {
		if reward.ItemSetID == 0 {
			continue
		}
		reward.Items = append([]QuestRewardItem(nil), rewardItemSets[reward.ItemSetID]...)
		rewards[questID] = reward
	}

	if len(rewards) == 0 {
		return nil, fmt.Errorf("quest.dec does not contain quest rewards")
	}
	return rewards, nil
}

func parseQuestRewardItem(element xml.StartElement) (QuestRewardItem, error) {
	value, present := questAttribute(element, "item_id")
	if !present || value == "" {
		return QuestRewardItem{}, fmt.Errorf("reward item has no item_id")
	}
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return QuestRewardItem{}, fmt.Errorf("invalid reward item_id %q", value)
	}
	kind, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
	if err != nil || kind == 0 {
		return QuestRewardItem{}, fmt.Errorf("invalid reward item kind %q", parts[0])
	}
	option, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 32)
	if err != nil {
		return QuestRewardItem{}, fmt.Errorf("invalid reward item option %q", parts[1])
	}

	class := int64(0)
	if value, present, err := optionalQuestIntAttribute(element, "class"); err != nil {
		return QuestRewardItem{}, err
	} else if present {
		class = value
	}
	order := int64(0)
	if value, present, err := optionalQuestIntAttribute(element, "order"); err != nil {
		return QuestRewardItem{}, err
	} else if present {
		order = value
	}
	duration := int64(0)
	if value, present, err := optionalQuestIntAttribute(element, "duration"); err != nil {
		return QuestRewardItem{}, err
	} else if present {
		duration = value
	}
	if class < 0 || class > 0xFF || order < 0 || order > 0xFFFF || duration < 0 || duration > 0xFF {
		return QuestRewardItem{}, fmt.Errorf("invalid reward item attributes on <%s>", element.Name.Local)
	}

	return QuestRewardItem{
		Kind:       uint32(kind),
		Option:     int32(option),
		DurationID: byte(duration),
		Class:      byte(class),
		Order:      uint16(order),
	}, nil
}

func parseQuestRewardValues(value string) ([questRewardValueCount]int64, error) {
	var values [questRewardValueCount]int64
	parts := strings.Split(value, ",")
	if len(parts) != questRewardValueCount {
		return values, fmt.Errorf("invalid reward value count: got %d, want %d", len(parts), questRewardValueCount)
	}
	for index, part := range parts {
		part = strings.TrimSpace(part)
		parsed, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return values, fmt.Errorf("invalid reward value %q: %w", part, err)
		}
		if parsed < 0 {
			return values, fmt.Errorf("negative reward value %d", parsed)
		}
		values[index] = parsed
	}
	return values, nil
}

func questAttribute(element xml.StartElement, name string) (string, bool) {
	for _, attribute := range element.Attr {
		if attribute.Name.Local == name {
			return attribute.Value, true
		}
	}
	return "", false
}

func parseQuestNPCActions(data []byte) (map[questNPCActionKey]QuestNPCAction, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	actions := make(map[questNPCActionKey]QuestNPCAction)

	var questID uint16
	var inQuest bool
	var setID uint16
	var inNPCSet bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse quest NPC actions: %w", err)
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "quest":
				id, present, err := optionalUintAttribute(element, "quest_id", 16)
				if err != nil {
					return nil, err
				}
				if present {
					if id == 0 {
						return nil, fmt.Errorf("invalid quest id %d", id)
					}
					questID = uint16(id)
					inQuest = true
				}
			case "npcs":
				if !inQuest {
					continue
				}
				id, present, err := optionalUintAttribute(element, "npcset_id", 16)
				if err != nil {
					return nil, err
				}
				if !present || id == 0 {
					return nil, fmt.Errorf("quest %d has invalid npcset_id", questID)
				}
				setID = uint16(id)
				inNPCSet = true
			case "quest_npc":
				if !inQuest || !inNPCSet {
					continue
				}
				action, err := parseQuestNPCAction(element, questID, setID)
				if err != nil {
					return nil, err
				}
				key := questNPCActionKey{questID: questID, setID: setID, index: action.Index}
				if _, exists := actions[key]; exists {
					return nil, fmt.Errorf("duplicate quest NPC action: quest=%d set=%d index=%d", questID, setID, action.Index)
				}
				actions[key] = action
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "npcs":
				inNPCSet = false
			case "quest":
				if inQuest {
					inQuest = false
				}
			}
		}
	}

	if len(actions) == 0 {
		return nil, fmt.Errorf("quest.dec does not contain quest NPC actions")
	}
	return actions, nil
}

func parseQuestNPCAction(element xml.StartElement, questID, setID uint16) (QuestNPCAction, error) {
	index, present, err := optionalQuestIntAttribute(element, "unique_order")
	if err != nil {
		return QuestNPCAction{}, err
	}
	if !present || index < 0 {
		return QuestNPCAction{}, fmt.Errorf("quest %d set %d has invalid unique_order", questID, setID)
	}

	order, present, err := optionalQuestIntAttribute(element, "npc_action_order")
	if err != nil {
		return QuestNPCAction{}, err
	}
	if !present {
		// Some client revisions use the shorter attribute name.
		order, present, err = optionalQuestIntAttribute(element, "action_order")
		if err != nil {
			return QuestNPCAction{}, err
		}
	}
	if !present || order < 0 {
		return QuestNPCAction{}, fmt.Errorf("quest %d set %d action %d has invalid action order", questID, setID, index)
	}

	actionType, present, err := optionalUintAttribute(element, "action_type", 8)
	if err != nil {
		return QuestNPCAction{}, err
	}
	if !present {
		return QuestNPCAction{}, fmt.Errorf("quest %d set %d action %d has no action type", questID, setID, index)
	}

	values, err := parseQuestNPCValues(element)
	if err != nil {
		return QuestNPCAction{}, err
	}

	return QuestNPCAction{
		QuestID:    questID,
		SetID:      setID,
		Index:      int32(index),
		Order:      int32(order),
		ActionType: byte(actionType),
		Values:     values,
	}, nil
}

func parseQuestNPCValues(element xml.StartElement) ([3]int32, error) {
	var values [3]int32
	value := ""
	for _, attribute := range element.Attr {
		if attribute.Name.Local == "value" {
			value = attribute.Value
			break
		}
	}
	if value == "" {
		return values, nil
	}

	parts := strings.Split(value, ",")
	if len(parts) > len(values) {
		return values, fmt.Errorf("too many quest NPC action values on <%s>", element.Name.Local)
	}
	for index, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			return values, fmt.Errorf("invalid quest NPC action value %q: %w", part, err)
		}
		values[index] = int32(parsed)
	}
	return values, nil
}

func optionalQuestIntAttribute(element xml.StartElement, name string) (int64, bool, error) {
	for _, attribute := range element.Attr {
		if attribute.Name.Local != name {
			continue
		}
		if attribute.Value == "" {
			return 0, false, nil
		}
		value, err := strconv.ParseInt(attribute.Value, 10, 32)
		if err != nil {
			return 0, false, fmt.Errorf("invalid %s attribute %q on <%s>: %w",
				name, attribute.Value, element.Name.Local, err)
		}
		return value, true, nil
	}
	return 0, false, nil
}
