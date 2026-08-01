package rpc

import (
	"errors"

	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/rpc"
)

// SaveSkillExperience persists skill progress, rank promotions and the stat
// and vital changes caused by a promotion in one optimistic update.
func SaveSkillExperience(_ *rpc.Client, r *character.SkillExperienceReq, s *character.SkillExperienceRes) error {
	s.Result = false
	if r == nil || r.Character <= 0 ||
		r.SwordRank == 0 || r.SwordRank > 10 || r.MagicRank == 0 || r.MagicRank > 10 ||
		r.SwordRank < r.ExpectedSwordRank || r.MagicRank < r.ExpectedMagicRank ||
		r.SwordPoint < r.ExpectedSwordPoint || r.MagicPoint < r.ExpectedMagicPoint ||
		r.STR < r.ExpectedSTR || r.DEX < r.ExpectedDEX || r.INT < r.ExpectedINT ||
		r.MaxHP == 0 || r.CurrentHP > r.MaxHP || r.MaxMP == 0 || r.CurrentMP > r.MaxMP {
		return errors.New("invalid skill experience update")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	result, err := db.Exec(
		"UPDATE characters SET "+
			"sword_rank = ?, magic_rank = ?, sword_exp = ?, magic_exp = ?, "+
			"sword_point = ?, magic_point = ?, sword_rank_exp = ?, magic_rank_exp = ?, "+
			"str_stat = ?, dex_stat = ?, int_stat = ?, "+
			"current_hp = ?, max_hp = ?, current_mp = ?, max_mp = ? "+
			"WHERE id = ? AND sword_rank = ? AND magic_rank = ? AND "+
			"sword_exp = ? AND magic_exp = ? AND sword_point = ? AND magic_point = ? AND "+
			"sword_rank_exp = ? AND magic_rank_exp = ? AND str_stat = ? AND dex_stat = ? AND "+
			"int_stat = ? AND current_hp = ? AND max_hp = ? AND current_mp = ? AND max_mp = ?",
		r.SwordRank, r.MagicRank, r.SwordExp, r.MagicExp,
		r.SwordPoint, r.MagicPoint, r.SwordRankExp, r.MagicRankExp,
		r.STR, r.DEX, r.INT, r.CurrentHP, r.MaxHP, r.CurrentMP, r.MaxMP,
		r.Character,
		r.ExpectedSwordRank, r.ExpectedMagicRank, r.ExpectedSwordExp, r.ExpectedMagicExp,
		r.ExpectedSwordPoint, r.ExpectedMagicPoint, r.ExpectedSwordRankExp, r.ExpectedMagicRankExp,
		r.ExpectedSTR, r.ExpectedDEX, r.ExpectedINT,
		r.ExpectedCurrentHP, r.ExpectedMaxHP, r.ExpectedCurrentMP, r.ExpectedMaxMP,
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
