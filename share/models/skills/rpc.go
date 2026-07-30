package skills

type SkillRequest struct {
	Server        byte
	Id            int32
	PreviousLevel byte
	Skill         Skill
}

type SkillResponse struct {
	Result bool
}

type LearnRequest struct {
	Server        byte
	Character     int32
	InventorySlot uint16
	ItemID        uint32
	Skill         Skill
}

type UntrainRequest struct {
	Server        byte
	Character     int32
	SkillID       uint16
	Slot          uint16
	PreviousLevel byte
	Price         uint64
}

type UntrainResponse struct {
	Result bool
	Level  byte
	Alz    uint64
}
