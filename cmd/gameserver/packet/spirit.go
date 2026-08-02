package packet

import "github.com/ubis/Freya/cmd/gameserver/context"

const spiritPointsPerSuccessfulHit uint32 = 1000

// addSpiritPoints awards SP for successful attack targets and keeps the
// mutation behind the character mutex.
func addSpiritPoints(ctx *context.Context, successfulHits uint32) bool {
	if ctx == nil || successfulHits == 0 {
		return false
	}

	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()
	if ctx.Char == nil {
		return false
	}

	return ctx.Char.AddSpiritPoints(successfulHits * spiritPointsPerSuccessfulHit)
}
