package packet

import (
	"fmt"
	"time"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const battleModeSkillGroup byte = 32

const (
	battleModeDrainJob      = "battle_mode_drain"
	battleModeDrainInterval = 2 * time.Second
)

type battleModeFailure byte

const (
	battleModeFailureNone battleModeFailure = iota
	battleModeFailureSpirit
	battleModeFailureMana
)

type battleModeSkillResult struct {
	Accepted  bool
	Failure   battleModeFailure
	Start     uint16
	CurrentMP uint16
	CurrentSP uint16
	ID        int32
	Style     uint32
	LiveStyle byte
	StyleEx   byte
	Save      bool
}

// handleBattleModeSkill implements the short SkillToUser branch used by
// group-32 skills. The values and flags follow WorldSvr's OnSTUSkillG032:
// one activation spends one SP lamp (5000 SP), MP comes from cabal.dec, and
// the notification carries the Battle Mode styleEx bit.
func handleBattleModeSkill(ctx *context.Context, skillID, slot, requested uint16) (battleModeSkillResult, bool) {
	meta, ok := clientdata.FindSkillMeta(skillID)
	if !ok || meta.Group != battleModeSkillGroup {
		return battleModeSkillResult{}, false
	}

	result := battleModeSkillResult{}
	var timerToStop *time.Timer
	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()
	if ctx.Char == nil {
		return result, true
	}

	result.ID = ctx.Char.Id
	result.CurrentMP = ctx.Char.CurrentMP
	result.CurrentSP = ctx.Char.CurrentSP
	result.Style = ctx.Char.Style.Get()
	result.LiveStyle = byte(ctx.Char.LiveStyle)
	result.StyleEx = ctx.BattleMode.StyleEx()

	modeType, ok := clientdata.BattleModeTypeForSkill(ctx.Char.Style.BattleStyle, skillID)
	learned := ctx.Char.Skills.Get(slot)
	if !ok || meta.Exclusive != ctx.Char.Style.BattleStyle || learned.Id != skillID || learned.Level == 0 {
		log.Warningf("Rejecting Battle Mode skill: character=%d skill=%d slot=%d", ctx.Char.Id, skillID, slot)
		return result, true
	}

	if requested != 0 {
		if ctx.BattleMode.Active() {
			log.Warningf("Rejecting Battle Mode start while another mode is active: character=%d skill=%d", ctx.Char.Id, skillID)
			return result, true
		}
		if ctx.Char.CurrentSP < character.SpiritPointsPerLamp {
			log.Warningf("Rejecting Battle Mode start due to SP: character=%d skill=%d sp=%d", ctx.Char.Id, skillID, ctx.Char.CurrentSP)
			result.Failure = battleModeFailureSpirit
			return result, true
		}

		mpWaste := meta.MPWaste(learned.Level)
		if ctx.Char.CurrentMP < mpWaste {
			log.Warningf("Rejecting Battle Mode start due to MP: character=%d skill=%d mp=%d required=%d", ctx.Char.Id, skillID, ctx.Char.CurrentMP, mpWaste)
			result.Failure = battleModeFailureMana
			return result, true
		}

		ctx.Char.CurrentSP -= character.SpiritPointsPerLamp
		ctx.Char.CurrentMP -= mpWaste
		ctx.BattleModeGeneration++
		ctx.BattleMode = context.BattleModeState{
			Type:           modeType,
			SkillID:        skillID,
			MPWastePercent: meta.BattleModeMPWaste(ctx.Char.Style.MasteryLevel, modeType),
			RemainingSP:    int(character.SpiritPointsPerLamp),
			SPWaste:        meta.SPWaste,
			Generation:     ctx.BattleModeGeneration,
		}
		result.Start = 1
		result.Save = true
	} else {
		if ctx.BattleMode.Active() && ctx.BattleMode.Type != modeType {
			log.Warningf("Rejecting Battle Mode stop for inactive mode: character=%d skill=%d", ctx.Char.Id, skillID)
			return result, true
		}
		timerToStop = ctx.BattleModeEndTimer
		ctx.BattleModeEndTimer = nil
		ctx.BattleMode = context.BattleModeState{}
	}
	if timerToStop != nil {
		timerToStop.Stop()
	}

	result.Accepted = true
	result.CurrentMP = ctx.Char.CurrentMP
	result.CurrentSP = ctx.Char.CurrentSP
	result.Style = ctx.Char.Style.Get()
	result.LiveStyle = byte(ctx.Char.LiveStyle)
	result.StyleEx = ctx.BattleMode.StyleEx()
	return result, true
}

func startBattleModeDrain(session *network.Session, ctx *context.Context) {
	ctx.Mutex.RLock()
	generation := ctx.BattleMode.Generation
	remainingSP := ctx.BattleMode.RemainingSP
	spWaste := ctx.BattleMode.SPWaste
	skillID := ctx.BattleMode.SkillID
	characterID := int32(0)
	preparationDuration := time.Duration(0)
	if meta, ok := clientdata.FindSkillMeta(skillID); ok && meta.PreparationMS > 0 {
		preparationDuration = time.Duration(meta.PreparationMS) * time.Millisecond
	}
	duration := battleModeDuration(remainingSP, spWaste, preparationDuration)
	if ctx.Char != nil {
		characterID = ctx.Char.Id
	}
	ctx.Mutex.RUnlock()

	session.RemoveJob(battleModeDrainJob)
	firstDrainAt := time.Now().Add(preparationDuration + battleModeDrainInterval)
	endTimer := time.AfterFunc(duration, func() {
		endedSkillID, ended := finishBattleMode(ctx, generation)
		if !ended {
			return
		}
		session.RemoveJob(battleModeDrainJob)
		log.Infof("Battle Mode ended: character=%d skill=%d", battleModeCharacterID(ctx), endedSkillID)
		notifyBattleModeEnded(session, ctx, endedSkillID)
	})

	active := false
	ctx.Mutex.Lock()
	if ctx.BattleMode.Active() && ctx.BattleMode.Generation == generation {
		ctx.BattleModeEndTimer = endTimer
		active = true
	} else {
		endTimer.Stop()
	}
	ctx.Mutex.Unlock()
	if !active {
		return
	}
	log.Infof("Battle Mode started: character=%d skill=%d preparation=%s duration=%s", characterID, skillID, preparationDuration, duration)

	firstTick := true
	var lastDrain time.Time
	task := network.NewPeriodicTask(battleModeDrainInterval, func() {
		// NewPeriodicTask invokes the callback immediately. WorldSvr waits for
		// the preparation animation and one SP interval before consuming the
		// first part of the mode gauge.
		if firstTick {
			firstTick = false
			return
		}
		if time.Now().Before(firstDrainAt) {
			return
		}
		// PeriodicTask invokes the callback once from the ticker branch and
		// once again at the top of its loop. Keep the server-side interval at
		// the same 2 seconds as WorldSvr.
		if !lastDrain.IsZero() && time.Since(lastDrain) < battleModeDrainInterval {
			return
		}
		lastDrain = time.Now()

		ended, skillID := consumeBattleModeGauge(ctx, generation)
		if !ended {
			return
		}

		// Stop is sent asynchronously because PeriodicTask.Stop waits for the
		// task goroutine to reach its select after this callback returns.
		go session.RemoveJob(battleModeDrainJob)
		if skillID != 0 {
			log.Infof("Battle Mode ended: character=%d skill=%d", battleModeCharacterID(ctx), skillID)
			notifyBattleModeEnded(session, ctx, skillID)
		}
	})
	session.AddJob(battleModeDrainJob, task)
}

func battleModeDuration(remainingSP, spWaste int, preparation time.Duration) time.Duration {
	gaugeDuration := battleModeDrainInterval
	if remainingSP <= 0 {
		return preparation + gaugeDuration
	}
	if spWaste > 0 {
		ticks := (remainingSP + spWaste - 1) / spWaste
		// The client considers the gauge exhausted on the last drain. Do not
		// keep the mode alive for an additional empty 2-second interval.
		if ticks > 1 {
			ticks--
		}
		gaugeDuration = time.Duration(ticks) * battleModeDrainInterval
	}
	return preparation + gaugeDuration
}

func battleModeCharacterID(ctx *context.Context) int32 {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()
	if ctx.Char == nil {
		return 0
	}
	return ctx.Char.Id
}

// resetBattleMode is used when the character leaves the world. Battle mode
// is runtime state and must not survive a return to the character lobby.
func resetBattleMode(session *network.Session, ctx *context.Context) {
	session.RemoveJob(battleModeDrainJob)

	ctx.Mutex.Lock()
	timer := ctx.BattleModeEndTimer
	ctx.BattleModeEndTimer = nil
	ctx.BattleModeGeneration++
	ctx.BattleMode = context.BattleModeState{}
	ctx.Mutex.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

func finishBattleMode(ctx *context.Context, generation uint64) (uint16, bool) {
	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()

	state := ctx.BattleMode
	if !state.Active() || state.Generation != generation {
		return 0, false
	}

	ctx.BattleModeEndTimer = nil
	ctx.BattleMode = context.BattleModeState{}
	return state.SkillID, true
}

func consumeBattleModeGauge(ctx *context.Context, generation uint64) (bool, uint16) {
	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()

	state := ctx.BattleMode
	if !state.Active() || state.Generation != generation {
		return true, 0
	}
	if state.SPWaste <= 0 {
		if ctx.BattleModeEndTimer != nil {
			ctx.BattleModeEndTimer.Stop()
			ctx.BattleModeEndTimer = nil
		}
		ctx.BattleMode = context.BattleModeState{}
		return true, state.SkillID
	}

	state.RemainingSP -= state.SPWaste
	if state.RemainingSP >= state.SPWaste {
		ctx.BattleMode = state
		return false, 0
	}

	ctx.BattleMode = context.BattleModeState{}
	if ctx.BattleModeEndTimer != nil {
		ctx.BattleModeEndTimer.Stop()
		ctx.BattleModeEndTimer = nil
	}
	return true, state.SkillID
}

func notifyBattleModeEnded(session *network.Session, ctx *context.Context, skillID uint16) {
	ctx.Mutex.RLock()
	if ctx.Char == nil || ctx.World == nil {
		ctx.Mutex.RUnlock()
		return
	}
	id := ctx.Char.Id
	style := ctx.Char.Style.Get()
	liveStyle := byte(ctx.Char.LiveStyle)
	currentSP := ctx.Char.CurrentSP
	world := ctx.World
	ctx.Mutex.RUnlock()

	// WorldSvr ends an automatically exhausted mode with the notification
	// below only. The client uses this packet to stop the local animation;
	// sending an extra SkillToUser response here makes it treat the stop as a
	// second skill exchange instead of an automatic mode cancellation.
	pkt := network.NewWriter(NFY_SKILLTOUSER)
	pkt.WriteUint16(skillID)
	pkt.WriteInt32(id)
	pkt.WriteUint32(style)
	pkt.WriteByte(liveStyle)
	pkt.WriteByte(0)
	pkt.WriteUint16(0)
	world.BroadcastSessionPacket(session, pkt)

	// This is WorldSvr's UT_SPDECEX notification emitted when the mode gauge
	// reaches zero. The character's regular SP was already charged on start.
	sendUpdatedData(session, updateTypeSPDrainEx, uint64(currentSP))
}

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
