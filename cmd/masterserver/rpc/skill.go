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

const (
	battleModeSkillSlotMin uint16 = 70
	battleModeSkillSlotMax uint16 = 76
)

func isBattleModeSkillSlot(slot uint16) bool {
	return slot >= battleModeSkillSlotMin && slot <= battleModeSkillSlotMax
}

// GrantBattleModeSkills reconciles mastery skills with their reserved
// client slots. It also repairs rows created earlier with an incorrect slot.
func GrantBattleModeSkills(_ *rpc.Client, r *skills.GrantBattleModeSkillsRequest, s *skills.GrantBattleModeSkillsResponse) error {
	s.Result = false
	s.Added = nil
	s.Removed = nil
	if r == nil || r.Character <= 0 {
		return errors.New("invalid battle mode skill request")
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

	desiredBySlot := make(map[uint16]uint16, len(r.Skills))
	for _, skill := range r.Skills {
		if skill.Id == 0 || skill.Level == 0 || !isBattleModeSkillSlot(skill.Slot) {
			return errors.New("invalid battle mode skill")
		}
		if previous, exists := desiredBySlot[skill.Slot]; exists && previous != skill.Id {
			return errors.New("duplicate battle mode skill slot")
		}
		desiredBySlot[skill.Slot] = skill.Id
	}

	var existingSkills []skills.Skill
	if err := tx.Select(&existingSkills,
		"SELECT skill, level, slot FROM characters_skills "+
			"WHERE id = ? AND slot BETWEEN ? AND ? FOR UPDATE",
		r.Character, battleModeSkillSlotMin, battleModeSkillSlotMax); err != nil {
		return err
	}
	for _, existing := range existingSkills {
		if desiredID, exists := desiredBySlot[existing.Slot]; exists && desiredID == existing.Id {
			continue
		}
		if _, err := tx.Exec(
			"DELETE FROM characters_skills WHERE id = ? AND slot = ?",
			r.Character, existing.Slot); err != nil {
			return err
		}
		s.Removed = append(s.Removed, existing.Slot)
	}

	for _, skill := range r.Skills {

		var existing skills.Skill
		err = tx.Get(&existing,
			"SELECT skill, level, slot FROM characters_skills WHERE id = ? AND skill = ? LIMIT 1 FOR UPDATE",
			r.Character, skill.Id)
		if err == nil {
			if existing.Slot == skill.Slot {
				continue
			}

			if !isBattleModeSkillSlot(skill.Slot) {
				continue
			}

			var target skills.Skill
			err = tx.Get(&target,
				"SELECT skill, level, slot FROM characters_skills WHERE id = ? AND slot = ? FOR UPDATE",
				r.Character, skill.Slot)
			if err == nil && target.Id != skill.Id {
				if _, err := tx.Exec(
					"DELETE FROM characters_skills WHERE id = ? AND slot = ?",
					r.Character, skill.Slot); err != nil {
					return err
				}
			} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			result, err := tx.Exec(
				"UPDATE characters_skills SET slot = ? WHERE id = ? AND skill = ? AND slot = ?",
				skill.Slot, r.Character, skill.Id, existing.Slot)
			if err != nil {
				return err
			}
			rows, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if rows == 1 {
				s.Added = append(s.Added, skills.Skill{
					Id:    skill.Id,
					Level: existing.Level,
					Slot:  skill.Slot,
				})
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		err = tx.Get(&existing,
			"SELECT skill, level, slot FROM characters_skills WHERE id = ? AND slot = ? FOR UPDATE",
			r.Character, skill.Slot)
		if err == nil {
			if !isBattleModeSkillSlot(skill.Slot) {
				continue
			}
			if _, err := tx.Exec(
				"DELETE FROM characters_skills WHERE id = ? AND slot = ?",
				r.Character, skill.Slot); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		if _, err := tx.Exec(
			"INSERT INTO characters_skills (id, skill, level, slot) VALUES (?, ?, ?, ?)",
			r.Character, skill.Id, skill.Level, skill.Slot); err != nil {
			return err
		}
		s.Added = append(s.Added, skill)
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

// UntrainSkill atomically lowers or removes a skill and charges its Alz price.
func UntrainSkill(_ *rpc.Client, r *skills.UntrainRequest, s *skills.UntrainResponse) error {
	s.Result = false
	if r.Character <= 0 || r.SkillID == 0 || r.PreviousLevel == 0 {
		return errors.New("invalid untrain skill request")
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

	var alz uint64
	if err := tx.Get(&alz, "SELECT alz FROM characters WHERE id = ? FOR UPDATE", r.Character); err != nil {
		return err
	}
	s.Alz = alz
	if alz < r.Price {
		return nil
	}

	var level byte
	err = tx.Get(&level,
		"SELECT level FROM characters_skills "+
			"WHERE id = ? AND skill = ? AND slot = ? FOR UPDATE",
		r.Character, r.SkillID, r.Slot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if level != r.PreviousLevel {
		return nil
	}

	nextLevel := level - 1
	var result sql.Result
	if nextLevel == 0 {
		result, err = tx.Exec(
			"DELETE FROM characters_skills "+
				"WHERE id = ? AND skill = ? AND slot = ? AND level = ?",
			r.Character, r.SkillID, r.Slot, level)
		if err == nil {
			_, err = tx.Exec(
				"DELETE FROM characters_quickslots WHERE id = ? AND skill = ?",
				r.Character, r.SkillID)
		}
	} else {
		result, err = tx.Exec(
			"UPDATE characters_skills SET level = ? "+
				"WHERE id = ? AND skill = ? AND slot = ? AND level = ?",
			nextLevel, r.Character, r.SkillID, r.Slot, level)
	}
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

	if r.Price != 0 {
		if _, err := tx.Exec(
			"UPDATE characters SET alz = alz - ? WHERE id = ?",
			r.Price, r.Character); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	s.Result = true
	s.Level = nextLevel
	s.Alz = alz - r.Price
	return nil
}
