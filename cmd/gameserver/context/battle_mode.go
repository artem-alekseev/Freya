package context

// BattleModeState is the runtime battle-mode state of a character.
// It is intentionally not persisted: the client must activate the mode again
// after a relogin, just as it does on the original server.
type BattleModeState struct {
	Type           byte
	SkillID        uint16
	MPWastePercent int
	RemainingSP    int
	SPWaste        int
	Generation     uint64
}

func (s BattleModeState) Active() bool {
	return s.Type == 1 || s.Type == 2
}

// StyleEx returns the bits used by WorldSvr for Battle Mode 1 and 2.
func (s BattleModeState) StyleEx() byte {
	switch s.Type {
	case 1:
		return 0x10
	case 2:
		return 0x20
	default:
		return 0
	}
}
