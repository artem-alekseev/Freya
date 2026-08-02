package rpc

import (
	"database/sql"
	"errors"

	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/rpc"
)

// OpenQuest atomically reserves an active quest slot for a character.
func OpenQuest(_ *rpc.Client, r *character.OpenQuestReq, s *character.OpenQuestRes) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Quest.QuestID == 0 || r.Quest.Slot >= character.QuestSlotCount {
		return errors.New("invalid quest opening request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existing character.ActiveQuest
	err = tx.Get(&existing,
		"SELECT quest_id, slot, transmuter_slot "+
			"FROM characters_quests WHERE id = ? AND slot = ? FOR UPDATE",
		r.Character, r.Quest.Slot)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var duplicate int
	if err := tx.Get(&duplicate,
		"SELECT COUNT(*) FROM characters_quests WHERE id = ? AND quest_id = ?",
		r.Character, r.Quest.QuestID); err != nil {
		return err
	}
	if duplicate != 0 {
		return nil
	}

	if _, err := tx.Exec(
		"INSERT INTO characters_quests (id, quest_id, slot, transmuter_slot, npc_flags) VALUES (?, ?, ?, ?, 0)",
		r.Character, r.Quest.QuestID, r.Quest.Slot, r.Quest.TransmuterSlot,
	); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	return nil
}

// SaveQuestUI persists the display state of an active quest.
func SaveQuestUI(_ *rpc.Client, r *character.QuestUIRequest, s *character.QuestUIResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Quest.QuestID == 0 || r.Quest.Slot >= character.QuestSlotCount {
		return errors.New("invalid quest UI request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	result, err := db.Exec(
		"UPDATE characters_quests SET show_desc = ?, expand = ? "+
			"WHERE id = ? AND quest_id = ? AND slot = ?",
		r.Quest.ShowDesc, r.Quest.Expand, r.Character, r.Quest.QuestID, r.Quest.Slot,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	s.Result = rows == 1
	return nil
}

// SaveQuestNPCFlags persists the completed NPC stages of an active quest.
// ExpectedNPCFlags prevents a stale packet from overwriting a newer stage.
func SaveQuestNPCFlags(_ *rpc.Client, r *character.SaveQuestNPCFlagsReq, s *character.SaveQuestNPCFlagsRes) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Quest.QuestID == 0 || r.Quest.Slot >= character.QuestSlotCount ||
		r.Quest.NPCFlags == r.ExpectedNPCFlags || r.Quest.NPCFlags&r.ExpectedNPCFlags != r.ExpectedNPCFlags {
		return errors.New("invalid quest NPC flags update")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	result, err := db.Exec(
		"UPDATE characters_quests SET npc_flags = ? "+
			"WHERE id = ? AND quest_id = ? AND slot = ? AND npc_flags = ?",
		r.Quest.NPCFlags, r.Character, r.Quest.QuestID, r.Quest.Slot, r.ExpectedNPCFlags,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	s.Result = rows == 1
	return nil
}

// CloseQuest removes a completed quest and applies its alz reward in the
// same database transaction. Experience and inventory are saved by the game
// server before this call.
func CloseQuest(_ *rpc.Client, r *character.CloseQuestReq, s *character.CloseQuestRes) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Quest.QuestID == 0 || r.Quest.Slot >= character.QuestSlotCount {
		return errors.New("invalid quest closing request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		"DELETE FROM characters_quests WHERE id = ? AND quest_id = ? AND slot = ?",
		r.Character, r.Quest.QuestID, r.Quest.Slot,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return nil
	}

	if r.Alz > 0 {
		result, err = tx.Exec(
			"UPDATE characters SET alz = alz + ? WHERE id = ?",
			r.Alz, r.Character,
		)
		if err != nil {
			return err
		}
		rows, err = result.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 1 {
			return nil
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	return nil
}
