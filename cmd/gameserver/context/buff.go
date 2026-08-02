package context

import "time"

// BuffEffect is a calculated force effect currently active on a character.
type BuffEffect struct {
	ForceID   int
	Value     int
	ValueType byte
}

// ActiveBuff keeps the runtime state needed to apply and expire a skill buff.
type ActiveBuff struct {
	SkillID    uint16
	Level      byte
	BuffType   uint16
	BuffKind   byte
	Generation uint64
	ExpiresAt  time.Time
	Effects    []BuffEffect
}

// ApplyBuff replaces the previous instance of the same skill and returns a
// generation token used by the expiration callback.
func (c *Context) ApplyBuff(skillID uint16, level byte, buffType uint16, buffKind byte, effects []BuffEffect, duration time.Duration) uint64 {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	c.BuffGeneration++
	generation := c.BuffGeneration
	remaining := c.Buffs[:0]
	for _, buff := range c.Buffs {
		if buff.SkillID != skillID || buff.BuffType != buffType || buff.BuffKind != buffKind {
			remaining = append(remaining, buff)
		}
	}

	clonedEffects := append([]BuffEffect(nil), effects...)
	active := ActiveBuff{
		SkillID:    skillID,
		Level:      level,
		BuffType:   buffType,
		BuffKind:   buffKind,
		Generation: generation,
		Effects:    clonedEffects,
	}
	if duration > 0 {
		active.ExpiresAt = time.Now().Add(duration)
	}
	c.Buffs = append(remaining, active)
	return generation
}

// RemoveBuff removes all matching instances and returns the remaining runtime
// buffs for the client stop-buff notification.
func (c *Context) RemoveBuff(skillID, buffType uint16, buffKind byte) (bool, []ActiveBuff) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	removed := false
	remaining := make([]ActiveBuff, 0, len(c.Buffs))
	for _, buff := range c.Buffs {
		if buff.SkillID == skillID && buff.BuffType == buffType && buff.BuffKind == buffKind {
			removed = true
			continue
		}
		remaining = append(remaining, buff)
	}
	if !removed {
		return false, nil
	}

	c.Buffs = remaining
	return true, append([]ActiveBuff(nil), remaining...)
}

// ExpireBuff removes a buff only when the callback still belongs to the
// current instance. Reapplying a skill therefore cannot be removed by an old
// timer callback.
func (c *Context) ExpireBuff(generation uint64) bool {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()

	for index, buff := range c.Buffs {
		if buff.Generation != generation {
			continue
		}
		copy(c.Buffs[index:], c.Buffs[index+1:])
		c.Buffs = c.Buffs[:len(c.Buffs)-1]
		return true
	}
	return false
}

// BuffValue returns the total active value for a force effect.
func (c *Context) BuffValue(forceID int) int {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()

	now := time.Now()
	result := 0
	for _, buff := range c.Buffs {
		if !buff.ExpiresAt.IsZero() && !now.Before(buff.ExpiresAt) {
			continue
		}
		for _, effect := range buff.Effects {
			if effect.ForceID == forceID {
				result += effect.Value
			}
		}
	}
	return result
}
