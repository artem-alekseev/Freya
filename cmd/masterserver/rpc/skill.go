package rpc

import (
	"database/sql"
	"errors"

	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/rpc"
)

func LearnSkill(_ *rpc.Client, r *skills.LearnRequest, s *skills.SkillResponse) error {
	s.Result = false
	if r.Character <= 0 || r.ItemID == 0 || r.Skill.Id == 0 || r.Skill.Level == 0 {
		return errors.New("invalid learn skill request")
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

	var itemID uint32
	err = tx.Get(&itemID,
		"SELECT kind FROM characters_inventory WHERE id = ? AND slot = ? FOR UPDATE",
		r.Character, r.InventorySlot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if itemID != r.ItemID {
		return nil
	}

	var conflicts int
	if err := tx.Get(&conflicts,
		"SELECT COUNT(*) FROM characters_skills WHERE id = ? AND (skill = ? OR slot = ?)",
		r.Character, r.Skill.Id, r.Skill.Slot); err != nil {
		return err
	}
	if conflicts != 0 {
		return nil
	}

	result, err := tx.Exec(
		"DELETE FROM characters_inventory WHERE id = ? AND slot = ? AND kind = ?",
		r.Character, r.InventorySlot, r.ItemID)
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

	if _, err := tx.Exec(
		"INSERT INTO characters_skills (id, skill, level, slot) VALUES (?, ?, ?, ?)",
		r.Character, r.Skill.Id, r.Skill.Level, r.Skill.Slot); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	return nil
}

// SaveSkill updates an existing character skill using optimistic locking.
func SaveSkill(c *rpc.Client, r *skills.SkillRequest, s *skills.SkillResponse) error {
	s.Result = false

	if r.Id <= 0 {
		return errors.New("character id is not set")
	}

	if r.PreviousLevel == ^byte(0) || r.Skill.Level != r.PreviousLevel+1 {
		return errors.New("invalid skill level transition")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	result, err := db.Exec(
		"UPDATE characters_skills SET level = ? "+
			"WHERE id = ? AND skill = ? AND slot = ? AND level = ?",
		r.Skill.Level, r.Id, r.Skill.Id, r.Skill.Slot, r.PreviousLevel,
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
