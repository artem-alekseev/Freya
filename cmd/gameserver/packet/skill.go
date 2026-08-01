package packet

import (
	"math"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

func QuickLinkSet(session *network.Session, reader *network.Reader) {
	slot := reader.ReadUint16()
	skill := reader.ReadUint16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ok, err := false, nil

	// removing quick link
	if skill == 0xFFFF {
		ok, err = ctx.Char.Links.Remove(slot)
	} else {
		ok, err = ctx.Char.Links.Set(slot, skills.Link{Skill: skill})
	}

	if err != nil {
		log.Error(err.Error())
	}

	pkt := network.NewWriter(QUICKLINKSET)
	pkt.WriteBool(ok)

	session.Send(pkt)
}

func QuickLinkSwap(session *network.Session, reader *network.Reader) {
	old := reader.ReadUint16()
	new := reader.ReadUint16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ok, err := ctx.Char.Links.Swap(old, new)
	if err != nil {
		log.Error(err.Error())
	}

	pkt := network.NewWriter(QUICKLINKSWAP)
	pkt.WriteBool(ok)

	session.Send(pkt)
}

func SkillToMobs(session *network.Session, reader *network.Reader) {
	const skillResultNormal = 2
	const targetTypeMob = 2

	wSkillIdx := reader.ReadUint16()
	_ = reader.ReadInt32() //bSlotIdx
	_ = reader.ReadByte()  //isMoving
	posX := reader.ReadInt16()
	posY := reader.ReadInt16()
	_ = reader.ReadByte() // timing
	_ = reader.ReadByte() //isMainTarget
	_ = reader.ReadByte()
	_ = reader.ReadByte()
	_ = reader.ReadByte()
	targetNum := reader.ReadByte()

	targets := make([]struct {
		Index  int
		HitNum byte
		MobHp  int
		PosX   uint16
		PosY   uint16
	}, targetNum)

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	if ctx.Char == nil || ctx.World == nil {
		ctx.Mutex.RUnlock()
		return
	}
	id := ctx.Char.Id
	ctx.Mutex.RUnlock()

	attack := calculateCharacterAttack(ctx)
	skillLevel := learnedSkillLevel(ctx, wSkillIdx)
	skillInfo, hasSkillInfo := clientdata.FindSkillDamage(wSkillIdx)
	skillDamage := attack.PhysicalMax
	if hasSkillInfo {
		skillDamage = skillInfo.Calculate(attack.PhysicalMax, 0, skillLevel)
	} else {
		log.Warningf("SkillToMobs: missing client damage data for skill %d, using physical attack %d",
			wSkillIdx, skillDamage)
	}
	log.Debugf("SkillToMobs: skill=%d level=%d baseAttack=%d damage=%d",
		wSkillIdx, skillLevel, attack.PhysicalMax, skillDamage)

	validTargets := targets[:0]
	var gainedExperience uint64
	seenTargets := make(map[int]struct{}, targetNum)
	for i := 0; i < int(targetNum); i++ {
		target := &targets[i]
		target.Index = int(reader.ReadInt32())
		_ = reader.ReadByte() // type
		target.HitNum = reader.ReadByte()
		_ = reader.ReadByte() // missMob

		if _, duplicate := seenTargets[target.Index]; duplicate {
			continue
		}
		seenTargets[target.Index] = struct{}{}

		mob := ctx.World.FindMob(target.Index)
		if mob == nil {
			continue
		}
		currentMobHP, _ := mob.GetHealth()
		if currentMobHP <= 0 {
			continue
		}

		if mob.SubHealth(skillDamage) {
			mobExperience := mob.GetExperience()
			if math.MaxUint64-gainedExperience < mobExperience {
				gainedExperience = math.MaxUint64
			} else {
				gainedExperience += mobExperience
			}
		}
		target.MobHp, _ = mob.GetHealth()
		mobPosition := mob.GetPosition()
		target.PosX = uint16(mobPosition.CurrentX)
		target.PosY = uint16(mobPosition.CurrentY)
		validTargets = append(validTargets, *target)
	}
	targets = validTargets
	skillExperience := skillExperienceResult{}
	if hasSkillInfo && len(targets) > 0 {
		skillExperience = addSkillExperience(ctx, skillInfo.Type, uint32(skillInfo.Experience())*skillExperienceMultiplier)
	}

	previousLevel, currentLevel, experienceUpdated, classRankUp := addExperience(ctx, gainedExperience)
	ctx.Mutex.RLock()
	currentHP := ctx.Char.CurrentHP
	currentMP := ctx.Char.CurrentMP
	currentSP := ctx.Char.CurrentSP
	experience := ctx.Char.Exp
	ctx.Mutex.RUnlock()

	//log.Debugf(
	//	"SkillToMob: skillID=%d, slot=%d, isMoving=%d, pos=(%d,%d), timing=%d, mainTarget=%d, targets=%d",
	//	wSkillIdx, bSlotIdx, isMoving, posX, posY, timing, isMainTarget, targetNum,
	//)
	//
	//log.Debug(targets)

	pkt := network.NewWriter(SKILLTOMOBS)
	// 1. Заголовок и ID скилла
	pkt.WriteUint16(wSkillIdx)

	pkt.WriteUint16(currentHP)
	pkt.WriteUint16(currentMP)
	pkt.WriteUint16(currentSP)
	pkt.WriteUint64(experience)
	pkt.WriteUint32(uint32(skillExperience.Applied)) // skill EXP
	pkt.WriteUint16(0)                               // Soul Ability Points
	pkt.WriteUint32(0)                               // Soul Ability EXP
	pkt.WriteUint32(0)                               // applyAbilityKind
	pkt.WriteUint32(0)                               // remained damage amp

	// 3. Количество целей
	pkt.WriteByte(uint8(len(targets)))

	// 4. Данные по каждой цели
	for _, target := range targets {
		pkt.WriteInt32(target.Index) // index
		pkt.WriteByte(targetTypeMob)
		pkt.WriteByte(skillResultNormal)
		pkt.WriteInt16(skillDamage)
		pkt.WriteInt32(target.MobHp)
		pkt.WriteUint16(target.PosX)
		pkt.WriteUint16(target.PosY)
		pkt.WriteByte(0)
		pkt.WriteUint32(0)
		pkt.WriteByte(target.HitNum)
	}
	session.Send(pkt)
	if experienceUpdated {
		if currentLevel > previousLevel {
			if err := ensureBattleModeSkillsForContext(ctx); err != nil {
				log.Errorf("Unable to grant battle mode skills for character %d: %s", id, err)
			}
		}
		sendExperienceUpdate(session, experience)
	}
	if experienceUpdated && currentLevel > previousLevel {
		sendLevelUpdate(session, currentLevel)
		sendHealthUpdate(session, currentHP)
		sendManaUpdate(session, currentMP)
		sendLevelUpEvent(session, id)
	}
	if experienceUpdated && classRankUp {
		sendClassRankUpEvent(session, id)
	}
	if skillExperience.SwordRankUp {
		sendSkillRankUpdate(session, 1, skillExperience.SwordRank)
		sendClassRankUpEvent(session, id)
	}
	if skillExperience.MagicRankUp {
		sendSkillRankUpdate(session, 2, skillExperience.MagicRank)
		sendClassRankUpEvent(session, id)
	}
	if skillExperience.SwordRankUp || skillExperience.MagicRankUp {
		sendHealthUpdate(session, skillExperience.CurrentHP)
		sendManaUpdate(session, skillExperience.CurrentMP)
	}

	npkt := network.NewWriter(NFY_SKILLTOMOBS)
	npkt.WriteUint16(wSkillIdx)

	npkt.WriteByte(1)

	// 3. Данные атакующего
	npkt.WriteInt32(id) // attackerCharIdx

	npkt.WriteInt16(posX)
	npkt.WriteInt16(posY)

	// 4. Количество целей

	// 5. Данные по целям
	for _, target := range targets {
		npkt.WriteInt32(target.Index) // index
		npkt.WriteInt32(targetTypeMob)
		npkt.WriteByte(skillResultNormal)
		npkt.WriteInt32(target.MobHp)
		npkt.WriteByte(0)
		npkt.WriteInt32(0)
		npkt.WriteByte(target.HitNum)
		npkt.WriteInt32(0)
		npkt.WriteUint16(target.PosX)
		npkt.WriteUint16(target.PosY)
	}

	ctx.World.BroadcastSessionPacket(session, npkt)
}

func learnedSkillLevel(ctx *context.Context, skillID uint16) byte {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	if ctx.Char == nil {
		return 1
	}
	for _, skill := range ctx.Char.Skills.List {
		if skill.Id == skillID && skill.Level > 0 {
			return skill.Level
		}
	}
	return 1
}

func addExperience(ctx *context.Context, gained uint64) (uint16, uint16, bool, bool) {
	const statPointsPerLevel = 5

	if gained == 0 {
		return 0, 0, false, false
	}

	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()
	if ctx.Char == nil || math.MaxUint64-ctx.Char.Exp < gained {
		return 0, 0, false, false
	}

	previousExperience := ctx.Char.Exp
	previousLevel := ctx.Char.Level
	currentExperience := previousExperience + gained
	currentLevel := clientdata.LevelForExperience(previousLevel, currentExperience)
	levelsGained := uint32(currentLevel - previousLevel)
	if levelsGained > math.MaxUint32/statPointsPerLevel {
		return 0, 0, false, false
	}
	statPoints := levelsGained * statPointsPerLevel
	if math.MaxUint32-ctx.Char.PNT < statPoints {
		return 0, 0, false, false
	}
	updatedCharacter := *ctx.Char
	updatedCharacter.Level = currentLevel
	currentClassRank := character.ClassRankForLevel(currentLevel)
	classRankChanged := currentClassRank != ctx.Char.Style.MasteryLevel
	classRankUp := currentClassRank > ctx.Char.Style.MasteryLevel
	updatedCharacter.Style.MasteryLevel = currentClassRank
	levelUp := currentLevel > previousLevel
	if levelUp || classRankChanged {
		updatedCharacter.CurrentHP = updatedCharacter.MaxHP
		updatedCharacter.RecalculateHP()
		updatedCharacter.CurrentHP = updatedCharacter.MaxHP
		updatedCharacter.CurrentMP = updatedCharacter.MaxMP
		updatedCharacter.RecalculateMP()
		updatedCharacter.CurrentMP = updatedCharacter.MaxMP
	}
	req := character.ExperienceReq{
		Server:        byte(g_ServerSettings.ServerId),
		Character:     ctx.Char.Id,
		ExpectedExp:   previousExperience,
		ExpectedLevel: previousLevel,
		Exp:           currentExperience,
		Level:         currentLevel,
		StatPoints:    statPoints,
	}
	if levelUp || classRankChanged {
		req.CurrentHP = updatedCharacter.CurrentHP
		req.MaxHP = updatedCharacter.MaxHP
		req.CurrentMP = updatedCharacter.CurrentMP
		req.MaxMP = updatedCharacter.MaxMP
	}
	res := character.ExperienceRes{}
	if err := g_RPCHandler.Call(rpc.SaveExperience, &req, &res); err != nil || !res.Result {
		if err != nil {
			log.Errorf("Unable to save experience for character %d: %s", req.Character, err.Error())
		}
		return 0, 0, false, false
	}

	ctx.Char.Exp = currentExperience
	ctx.Char.Level = currentLevel
	ctx.Char.Style.MasteryLevel = currentClassRank
	ctx.Char.PNT += statPoints
	if levelUp || classRankChanged {
		ctx.Char.CurrentHP = updatedCharacter.CurrentHP
		ctx.Char.MaxHP = updatedCharacter.MaxHP
		ctx.Char.CurrentMP = updatedCharacter.CurrentMP
		ctx.Char.MaxMP = updatedCharacter.MaxMP
	}
	return previousLevel, currentLevel, true, classRankUp
}

type skillExperienceResult struct {
	Applied     uint16
	SwordRankUp bool
	MagicRankUp bool
	SwordRank   byte
	MagicRank   byte
	CurrentHP   uint16
	CurrentMP   uint16
}

const skillExperienceMultiplier uint32 = 1000

func addSkillExperience(ctx *context.Context, skillType byte, gained uint32) skillExperienceResult {
	result := skillExperienceResult{}
	if gained == 0 || (skillType != 1 && skillType != 2) {
		return result
	}

	ctx.Mutex.Lock()
	defer ctx.Mutex.Unlock()
	if ctx.Char == nil {
		return result
	}

	updated := *ctx.Char
	currentExp := updated.SwordExp
	currentPoint := updated.SwordPoint
	currentRankExp := updated.SwordRankExp
	currentRank := updated.SwordRank
	if skillType == 2 {
		currentExp = updated.MagicExp
		currentPoint = updated.MagicPoint
		currentRankExp = updated.MagicRankExp
		currentRank = updated.MagicRank
	}

	if currentRank == 0 {
		currentRank = 1
		if skillType == 1 {
			updated.SwordRank = currentRank
		} else {
			updated.MagicRank = currentRank
		}
	}
	progress, ok := clientdata.FindSkillRankProgress(updated.Style.BattleStyle, currentRank, skillType)
	if !ok {
		log.Warningf("Unable to find skill rank data for style %d rank %d type %d",
			updated.Style.BattleStyle, currentRank, skillType)
		return result
	}
	requiredExp := skillRankExpForPoint(progress, skillType)
	requiredRankPoints := skillRankPointsForRank(progress, skillType)
	if requiredExp == 0 || (currentRank >= 10 && currentRankExp >= requiredRankPoints) {
		return result
	}

	totalExp := uint32(currentExp) + uint32(gained)
	acceptedExp := uint32(gained)
	for {
		progress, ok = clientdata.FindSkillRankProgress(updated.Style.BattleStyle, currentRank, skillType)
		if !ok {
			break
		}
		requiredExp = skillRankExpForPoint(progress, skillType)
		requiredRankPoints = skillRankPointsForRank(progress, skillType)
		if requiredExp == 0 {
			break
		}
		if totalExp < uint32(requiredExp) {
			break
		}

		totalExp -= uint32(requiredExp)
		if currentPoint < ^uint16(0) {
			currentPoint++
		}
		if currentRankExp < ^uint16(0) {
			currentRankExp++
		}

		if currentRankExp < requiredRankPoints {
			continue
		}
		if currentRank >= 10 {
			acceptedExp -= uint32(totalExp)
			totalExp = 0
			break
		}

		bonus, hasBonus := clientdata.FindSkillRankBonus(currentRank, skillType)
		if hasBonus {
			updated.STR += bonus.STR
			updated.DEX += bonus.DEX
			updated.INT += bonus.INT
		}
		currentRank++
		currentRankExp = 0
		if skillType == 1 {
			result.SwordRankUp = true
		} else {
			result.MagicRankUp = true
		}
	}

	if acceptedExp > uint32(^uint16(0)) {
		acceptedExp = uint32(^uint16(0))
	}
	if totalExp > uint32(^uint16(0)) {
		totalExp = uint32(^uint16(0))
	}

	if skillType == 1 {
		updated.SwordExp = uint16(totalExp)
		updated.SwordPoint = currentPoint
		updated.SwordRankExp = currentRankExp
		updated.SwordRank = currentRank
	} else {
		updated.MagicExp = uint16(totalExp)
		updated.MagicPoint = currentPoint
		updated.MagicRankExp = currentRankExp
		updated.MagicRank = currentRank
	}

	if result.SwordRankUp || result.MagicRankUp {
		updated.CurrentHP = updated.MaxHP
		updated.RecalculateHP()
		updated.CurrentHP = updated.MaxHP
		updated.CurrentMP = updated.MaxMP
		updated.RecalculateMP()
		updated.CurrentMP = updated.MaxMP
	}

	req := character.SkillExperienceReq{
		Server:               byte(g_ServerSettings.ServerId),
		Character:            ctx.Char.Id,
		ExpectedSwordRank:    ctx.Char.SwordRank,
		ExpectedMagicRank:    ctx.Char.MagicRank,
		ExpectedSwordExp:     ctx.Char.SwordExp,
		ExpectedMagicExp:     ctx.Char.MagicExp,
		ExpectedSwordPoint:   ctx.Char.SwordPoint,
		ExpectedMagicPoint:   ctx.Char.MagicPoint,
		ExpectedSwordRankExp: ctx.Char.SwordRankExp,
		ExpectedMagicRankExp: ctx.Char.MagicRankExp,
		ExpectedSTR:          ctx.Char.STR,
		ExpectedDEX:          ctx.Char.DEX,
		ExpectedINT:          ctx.Char.INT,
		ExpectedCurrentHP:    ctx.Char.CurrentHP,
		ExpectedMaxHP:        ctx.Char.MaxHP,
		ExpectedCurrentMP:    ctx.Char.CurrentMP,
		ExpectedMaxMP:        ctx.Char.MaxMP,
		SwordRank:            updated.SwordRank,
		MagicRank:            updated.MagicRank,
		SwordExp:             updated.SwordExp,
		MagicExp:             updated.MagicExp,
		SwordPoint:           updated.SwordPoint,
		MagicPoint:           updated.MagicPoint,
		SwordRankExp:         updated.SwordRankExp,
		MagicRankExp:         updated.MagicRankExp,
		STR:                  updated.STR,
		DEX:                  updated.DEX,
		INT:                  updated.INT,
		CurrentHP:            updated.CurrentHP,
		MaxHP:                updated.MaxHP,
		CurrentMP:            updated.CurrentMP,
		MaxMP:                updated.MaxMP,
	}
	res := character.SkillExperienceRes{}
	if err := g_RPCHandler.Call(rpc.SaveSkillExperience, &req, &res); err != nil || !res.Result {
		if err != nil {
			log.Errorf("Unable to save skill experience for character %d: %s", req.Character, err.Error())
		}
		return skillExperienceResult{}
	}

	ctx.Char.SwordRank = updated.SwordRank
	ctx.Char.MagicRank = updated.MagicRank
	ctx.Char.SwordExp = updated.SwordExp
	ctx.Char.MagicExp = updated.MagicExp
	ctx.Char.SwordPoint = updated.SwordPoint
	ctx.Char.MagicPoint = updated.MagicPoint
	ctx.Char.SwordRankExp = updated.SwordRankExp
	ctx.Char.MagicRankExp = updated.MagicRankExp
	ctx.Char.STR = updated.STR
	ctx.Char.DEX = updated.DEX
	ctx.Char.INT = updated.INT
	ctx.Char.CurrentHP = updated.CurrentHP
	ctx.Char.MaxHP = updated.MaxHP
	ctx.Char.CurrentMP = updated.CurrentMP
	ctx.Char.MaxMP = updated.MaxMP

	result.Applied = uint16(acceptedExp)
	result.SwordRank = updated.SwordRank
	result.MagicRank = updated.MagicRank
	result.CurrentHP = updated.CurrentHP
	result.CurrentMP = updated.CurrentMP
	return result
}

func skillRankExpForPoint(progress clientdata.SkillRankProgress, skillType byte) uint16 {
	if skillType == 1 {
		return progress.SwordExpForPoint
	}
	return progress.MagicExpForPoint
}

func skillRankPointsForRank(progress clientdata.SkillRankProgress, skillType byte) uint16 {
	if skillType == 1 {
		return progress.SwordRankPoints
	}
	return progress.MagicRankPoints
}

func sendSkillRankUpdate(session *network.Session, skillType, rank byte) {
	pkt := network.NewWriter(NFY_UPDATEDATAS)
	pkt.WriteByte(updateTypeRank)
	pkt.WriteUint16(0) // Soul Ability Points
	pkt.WriteUint32(0) // Soul Ability EXP
	pkt.WriteUint16(uint16(skillType))
	pkt.WriteUint16(uint16(rank))
	pkt.WriteUint32(0)
	session.Send(pkt)
}

func sendLevelUpEvent(session *network.Session, characterID int32) {
	ctx, err := context.Parse(session)
	if err != nil {
		return
	}

	pkt := network.NewWriter(NFY_CHARTREVENT)
	pkt.WriteByte(1) // EVT_LEVELUP
	pkt.WriteInt32(characterID)
	ctx.World.BroadcastSessionPacket(session, pkt)
}

func sendClassRankUpEvent(session *network.Session, characterID int32) {
	ctx, err := context.Parse(session)
	if err != nil {
		return
	}

	pkt := network.NewWriter(NFY_CHARTREVENT)
	pkt.WriteByte(2) // EVT_RANKUP
	pkt.WriteInt32(characterID)
	ctx.World.BroadcastSessionPacket(session, pkt)
}

func sendExperienceUpdate(session *network.Session, experience uint64) {
	sendUpdatedData(session, 8, experience) // UT_EXP
}

func sendLevelUpdate(session *network.Session, level uint16) {
	sendUpdatedData(session, 10, uint64(level)) // UT_LEVEL
}

func sendUpdatedData(session *network.Session, updateType byte, value uint64) {
	pkt := network.NewWriter(NFY_UPDATEDATAS)
	pkt.WriteByte(updateType)
	pkt.WriteUint16(0) // Soul Ability Points
	pkt.WriteUint32(0) // Soul Ability EXP
	pkt.WriteUint64(value)
	session.Send(pkt)
}
