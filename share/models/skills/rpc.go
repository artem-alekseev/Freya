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
