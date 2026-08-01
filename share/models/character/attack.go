package character

// AttackStats contains the physical attack range used by a normal attack.
//
// WorldSvr calculates the minimum physical attack as 4/5 of the maximum
// attack plus 1/20 of it. The style coefficients mirror the EP6
// BattleStyleData values and use the same 10000-based calculation as WorldSvr.
type AttackStats struct {
	PhysicalMin int
	PhysicalMax int
}

type attackStatCoefficient struct {
	str      int
	dex      int
	intStat  int
	masteryA int
	masteryB int
}

var attackStatCoefficients = [...]attackStatCoefficient{
	{},
	{str: 3000, dex: 2000, masteryA: 8, masteryB: -5},                // Warrior
	{str: 2500, dex: 2500, masteryA: 7, masteryB: -4},                // Blader
	{str: 2000, dex: 1000, intStat: 1000, masteryA: 5, masteryB: -4}, // Wizard
	{str: 2000, dex: 1200, intStat: 1000, masteryA: 5, masteryB: -5}, // Force Archer
	{str: 2500, dex: 2300, intStat: 1000, masteryA: 6, masteryB: -2}, // Force Shielder
	{str: 2400, dex: 2300, intStat: 1000, masteryA: 6, masteryB: -3}, // Force Blader
}

// CalculateAttack returns the current basic physical attack range.
func (c *Character) CalculateAttack() AttackStats {
	style := int(c.Style.BattleStyle)
	if style <= 0 || style >= len(attackStatCoefficients) {
		style = 1
	}

	coef := attackStatCoefficients[style]
	max := (coef.str*int(c.STR) + coef.dex*int(c.DEX) + coef.intStat*int(c.INT)) / 10000
	max += coef.masteryA*int(c.Style.MasteryLevel) + coef.masteryB
	if max < 1 {
		max = 1
	}

	min := max*4/5 + max/20
	if min < 1 {
		min = 1
	}
	if min > max {
		min = max
	}

	return AttackStats{PhysicalMin: min, PhysicalMax: max}
}
