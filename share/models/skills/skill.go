package skills

type Skill struct {
	Id    uint16 `db:"skill"`
	Level byte
	Slot  uint16
}

type GrantBattleModeSkillsRequest struct {
	Server    byte
	Character int32
	Skills    []Skill
}

type GrantBattleModeSkillsResponse struct {
	Result  bool
	Added   []Skill
	Removed []uint16
}
