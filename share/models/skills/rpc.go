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
