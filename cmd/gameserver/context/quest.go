package context

import "github.com/ubis/Freya/share/models/character"

// QuestSlotCount is the number of active quest slots supported by WorldSvr.
const QuestSlotCount = character.QuestSlotCount

// ActiveQuest contains the runtime state needed to identify an opened quest.
type ActiveQuest = character.ActiveQuest
