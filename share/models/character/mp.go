package character

const maxCharacterMP = ^uint16(0)

type mpFormula struct {
	initialMP uint32
	deltaMP   uint32
}

// These values are the target client's char_init/char_newhp values. The
// level and rank calculations mirror WorldSvr's GetBaseMP and rank-up paths.
var mpFormulas = [...]mpFormula{
	{},
	{initialMP: 20, deltaMP: 10},
	{initialMP: 20, deltaMP: 10},
	{initialMP: 30, deltaMP: 20},
	{initialMP: 30, deltaMP: 20},
	{initialMP: 25, deltaMP: 15},
	{initialMP: 25, deltaMP: 15},
}

// CalculateMaxMP returns the calculated maximum MP without mutating the
// character. Battle-style ranks are one-based in the Go database model, so
// rank 1 (Novice) is the baseline and later ranks add the WorldSvr bonuses.
func (c Character) CalculateMaxMP() uint16 {
	style := c.Style.BattleStyle
	if style == 0 || int(style) >= len(mpFormulas) {
		return c.MaxMP
	}

	formula := mpFormulas[style]
	level := uint32(c.Level)
	if level == 0 {
		level = 1
	}

	maxMP := formula.initialMP + formula.deltaMP*(level-1)/10
	maxMP += rankMPBonus(c.SwordRank, true)
	maxMP += rankMPBonus(c.MagicRank, false)

	if maxMP == 0 {
		return 1
	}
	if maxMP > uint32(maxCharacterMP) {
		return maxCharacterMP
	}
	return uint16(maxMP)
}

// RecalculateMP updates MaxMP and preserves the current mana percentage.
// A zero/empty value is treated like the WorldSvr login path and restored to
// full mana. It returns true when either persisted value changed.
func (c *Character) RecalculateMP() bool {
	if c == nil {
		return false
	}

	oldCurrentMP := c.CurrentMP
	oldMaxMP := c.MaxMP
	newMaxMP := c.CalculateMaxMP()

	var newCurrentMP uint16
	switch {
	case oldMaxMP == 0 || oldCurrentMP == 0 || oldCurrentMP >= oldMaxMP:
		newCurrentMP = newMaxMP
	default:
		newCurrentMP = uint16(uint32(oldCurrentMP) * uint32(newMaxMP) / uint32(oldMaxMP))
		if newCurrentMP == 0 {
			newCurrentMP = 1
		}
	}

	c.MaxMP = newMaxMP
	c.CurrentMP = newCurrentMP
	return oldCurrentMP != c.CurrentMP || oldMaxMP != c.MaxMP
}

func rankMPBonus(rank byte, sword bool) uint32 {
	if rank <= 1 {
		return 0
	}

	bonus := uint32(0)
	for previousRank := uint32(1); previousRank < uint32(rank); previousRank++ {
		if sword {
			bonus += 5 * (previousRank + 1)
		} else {
			bonus += 10 * (previousRank + 1)
		}
	}
	return bonus
}
