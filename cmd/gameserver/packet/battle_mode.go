package packet

import (
	"fmt"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/rpc"
)

func ensureBattleModeSkillsForCharacter(characterID int32, level uint16, style byte, skillList *skills.SkillList) error {
	if skillList == nil {
		return fmt.Errorf("skill list is not initialized")
	}

	changes, err := requestBattleModeSkills(characterID, level, style)
	if err != nil {
		return err
	}
	applyBattleModeSkills(skillList, changes)
	return nil
}

func ensureBattleModeSkillsForContext(ctx *context.Context) error {
	ctx.Mutex.RLock()
	if ctx.Char == nil {
		ctx.Mutex.RUnlock()
		return fmt.Errorf("character is not initialized")
	}
	characterID := ctx.Char.Id
	level := ctx.Char.Level
	style := ctx.Char.Style.BattleStyle
	ctx.Mutex.RUnlock()

	changes, err := requestBattleModeSkills(characterID, level, style)
	if err != nil {
		return err
	}

	if len(changes.Added) != 0 || len(changes.Removed) != 0 {
		ctx.Mutex.Lock()
		applyBattleModeSkills(&ctx.Char.Skills, changes)
		ctx.Mutex.Unlock()
	}
	return nil
}

func applyBattleModeSkills(skillList *skills.SkillList, changes skills.GrantBattleModeSkillsResponse) {
	if skillList == nil || skillList.List == nil {
		return
	}

	for _, slot := range changes.Removed {
		skillList.Remove(slot)
	}

	for _, skill := range changes.Added {
		for slot, current := range skillList.List {
			if current.Id == skill.Id && slot != int(skill.Slot) {
				delete(skillList.List, slot)
				continue
			}
			if slot == int(skill.Slot) && current.Id != skill.Id {
				delete(skillList.List, slot)
			}
		}
		skillList.Set(skill.Slot, skill)
	}
}

func requestBattleModeSkills(characterID int32, level uint16, style byte) (skills.GrantBattleModeSkillsResponse, error) {
	desired := clientdata.FindBattleModeSkillsForLevel(style, level)
	required := make([]skills.Skill, 0, len(desired))
	for _, desiredSkill := range desired {
		required = append(required, skills.Skill{
			Id:    desiredSkill.SkillID,
			Level: 1,
			Slot:  desiredSkill.Slot,
		})
	}

	req := skills.GrantBattleModeSkillsRequest{
		Server:    byte(g_ServerSettings.ServerId),
		Character: characterID,
		Skills:    required,
	}
	res := skills.GrantBattleModeSkillsResponse{}
	if err := g_RPCHandler.Call(rpc.GrantBattleModeSkills, &req, &res); err != nil {
		return skills.GrantBattleModeSkillsResponse{}, err
	}
	if !res.Result {
		return skills.GrantBattleModeSkillsResponse{}, fmt.Errorf("battle mode skills were not saved")
	}
	return res, nil
}
