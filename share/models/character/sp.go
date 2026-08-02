package character

const (
	SpiritPointsPerLamp uint16 = 5000
	maxSpiritLamps      byte   = 5
)

// CalculateMaxSP returns the number of SP points available at the current
// class rank. Rank 2 unlocks the first 5000-point lamp; the server supports
// up to five lamps.
func (c Character) CalculateMaxSP() uint16 {
	if c.Style.MasteryLevel < 2 {
		return 0
	}

	lamps := c.Style.MasteryLevel - 1
	if lamps > maxSpiritLamps {
		lamps = maxSpiritLamps
	}
	return uint16(lamps) * SpiritPointsPerLamp
}

// RecalculateSP updates the maximum SP after a rank change and clamps the
// current value if the available maximum has decreased.
func (c *Character) RecalculateSP() bool {
	if c == nil {
		return false
	}

	oldCurrentSP := c.CurrentSP
	oldMaxSP := c.MaxSP
	c.MaxSP = c.CalculateMaxSP()
	if c.CurrentSP > c.MaxSP {
		c.CurrentSP = c.MaxSP
	}
	return oldCurrentSP != c.CurrentSP || oldMaxSP != c.MaxSP
}

// AddSpiritPoints adds SP without exceeding the character's current maximum.
func (c *Character) AddSpiritPoints(amount uint32) bool {
	if c == nil || amount == 0 || c.CurrentSP >= c.MaxSP {
		return false
	}

	newCurrentSP := uint32(c.CurrentSP) + amount
	if newCurrentSP > uint32(c.MaxSP) {
		newCurrentSP = uint32(c.MaxSP)
	}
	if newCurrentSP == uint32(c.CurrentSP) {
		return false
	}

	c.CurrentSP = uint16(newCurrentSP)
	return true
}
