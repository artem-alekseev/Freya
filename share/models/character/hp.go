package character

const maxCharacterHP = ^uint16(0)

type hpFormula struct {
	initialHP     uint32
	deltaHP       uint32
	masteryBDelta uint32
}

// These values are the target client's char_init/char_newhp values. The
// level and style-mastery calculations mirror WorldSvr's GetBaseHP and
// BattleStyleHpBoostAdd paths.
var hpFormulas = [...]hpFormula{
	{},
	{initialHP: 50, deltaHP: 30, masteryBDelta: 500},
	{initialHP: 50, deltaHP: 30, masteryBDelta: 389},
	{initialHP: 40, deltaHP: 20, masteryBDelta: 79},
	{initialHP: 40, deltaHP: 20, masteryBDelta: 184},
	{initialHP: 45, deltaHP: 25, masteryBDelta: 310},
	{initialHP: 45, deltaHP: 25, masteryBDelta: 310},
}

// CalculateMaxHP returns the calculated maximum HP without mutating the
// character. Battle-style ranks are one-based in the Go database model, so
// rank 1 (Novice) is the baseline and later ranks add the WorldSvr bonuses.
func (c Character) CalculateMaxHP() uint16 {
	style := c.Style.BattleStyle
	if style == 0 || int(style) >= len(hpFormulas) {
		return c.MaxHP
	}

	formula := hpFormulas[style]
	level := uint32(c.Level)
	if level == 0 {
		level = 1
	}

	maxHP := formula.initialHP + formula.deltaHP*(level-1)/10
	maxHP += rankHPBonus(c.SwordRank, true)
	maxHP += rankHPBonus(c.MagicRank, false)

	masteryLevel := uint32(c.Style.MasteryLevel)
	if masteryLevel >= 8 {
		masteryFactor := 5*masteryLevel*masteryLevel - 14*masteryLevel + 9
		maxHP += formula.masteryBDelta * masteryFactor / 500
	}

	if maxHP == 0 {
		return 1
	}
	if maxHP > uint32(maxCharacterHP) {
		return maxCharacterHP
	}
	return uint16(maxHP)
}

// RecalculateHP updates MaxHP and preserves the current health percentage.
// A zero/empty value is treated like the WorldSvr login path and restored to
// full health. It returns true when either persisted value changed.
func (c *Character) RecalculateHP() bool {
	if c == nil {
		return false
	}

	oldCurrentHP := c.CurrentHP
	oldMaxHP := c.MaxHP
	newMaxHP := c.CalculateMaxHP()

	var newCurrentHP uint16
	switch {
	case oldMaxHP == 0 || oldCurrentHP == 0 || oldCurrentHP >= oldMaxHP:
		newCurrentHP = newMaxHP
	default:
		newCurrentHP = uint16(uint32(oldCurrentHP) * uint32(newMaxHP) / uint32(oldMaxHP))
		if newCurrentHP == 0 {
			newCurrentHP = 1
		}
	}

	c.MaxHP = newMaxHP
	c.CurrentHP = newCurrentHP
	return oldCurrentHP != c.CurrentHP || oldMaxHP != c.MaxHP
}

func rankHPBonus(rank byte, sword bool) uint32 {
	if rank <= 1 {
		return 0
	}

	bonus := uint32(0)
	for previousRank := uint32(1); previousRank < uint32(rank); previousRank++ {
		if sword {
			bonus += 10 * (previousRank + 1)
		} else {
			bonus += 5 * (previousRank + 1)
		}
	}
	return bonus
}
