package rpc

import (
	"errors"

	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/rpc"
)

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
