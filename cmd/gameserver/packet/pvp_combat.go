package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	attackToUserPacketSize = 18 // header + target user index + target character index
	userSkillTargetMinSize = 32 // header + one C2S_STUSKILL_TARGET
	maxPVPUserSkillTargets = 8
	// SR_MOBSDEAD in WorldSvr/TypedefEx.h. Result 4 is a successful buff
	// result, not the dead-target result.
	attackResultDead = 3
)

type pvpUserSkillTarget struct {
	userIndex int32
	hitNum    byte
}

func AttckToUser(session *network.Session, reader *network.Reader) {
	if reader.Size != attackToUserPacketSize {
		log.Warningf("[ATTCKTOUSER] invalid packet size: %d", reader.Size)
		return
	}

	targetUserIndex := decodeUserObjectIndex(reader.ReadInt32())
	targetCharacterID := reader.ReadInt32()
	if targetUserIndex < 0 || targetUserIndex > int32(^uint16(0)) ||
		targetUserIndex == int32(session.UserIdx) || targetCharacterID <= 0 {
		log.Warningf("[ATTCKTOUSER] invalid target: user=%d char=%d", targetUserIndex, targetCharacterID)
		sendPVPAttackResult(session, 0, 0, 0, 0, attackResultDead, 0, 0)
		return
	}

	targetSession := g_NetworkManager.GetSession(uint16(targetUserIndex))
	if targetSession == nil {
		log.Warningf("[ATTCKTOUSER] target session not found: user=%d char=%d", targetUserIndex, targetCharacterID)
		sendPVPAttackResult(session, 0, 0, 0, 0, attackResultDead, 0, 0)
		return
	}

	attackerContext, targetContext, unlock, ok := lockPVPCombatContexts(session, targetSession)
	if !ok {
		sendPVPAttackResult(session, 0, 0, 0, 0, attackResultDead, 0, 0)
		return
	}

	if !validPVPCombatPair(session, targetSession, attackerContext, targetContext, targetCharacterID) {
		log.Warningf("[ATTCKTOUSER] rejected attack: attacker=%d targetUser=%d targetChar=%d",
			session.UserIdx, targetUserIndex, targetCharacterID)
		unlock()
		sendPVPAttackResult(session, 0, 0, 0, 0, attackResultDead, 0, 0)
		return
	}

	attacker := attackerContext.Char
	target := targetContext.Char
	currentHP, currentMP, currentSP := attacker.CurrentHP, attacker.CurrentMP, attacker.CurrentSP
	if target.CurrentHP == 0 {
		unlock()
		sendPVPAttackResult(session, target.Id, currentHP, currentMP, currentSP, attackResultDead, 0, 0)
		return
	}

	attack := calculateCharacterAttackFromCharacter(attacker)
	damage := attack.PhysicalMax
	if damage > int(target.CurrentHP) {
		damage = int(target.CurrentHP)
	}
	target.CurrentHP -= uint16(damage)
	targetHP := target.CurrentHP
	attackerID := attacker.Id
	targetID := target.Id
	attackerWorld := attackerContext.World

	log.Debugf("[ATTCKTOUSER] attacker=%d target=%d damage=%d hp=%d", attackerID, targetID, damage, targetHP)

	// Build packets while both character states are still consistent. Sending is
	// done after unlocking below, so network callbacks cannot deadlock the world.
	response := newPVPAttackResult(
		targetID,
		currentHP,
		currentMP,
		currentSP,
		attackResultNormal,
		uint16(damage),
		targetHP,
	)
	motion := newPVPAttackMotion(uint16(damage))
	notification := newPVPAttackNotification(attackerID, targetID, attackResultNormal, targetHP)

	unlock()

	if !saveContextVitals(targetContext) {
		log.Warningf("[ATTCKTOUSER] unable to save target vitals: char=%d", targetID)
	}
	session.Send(response)
	targetSession.Send(motion)
	if attackerWorld != nil {
		attackerWorld.BroadcastSessionPacket(session, notification)
	}
}

func handleTargetedUserSkill(session *network.Session, reader *network.Reader) {
	skillID := reader.ReadUint16()
	slotValue := reader.ReadInt32()
	isMoving := int32(reader.ReadByte())
	posX := reader.ReadUint16()
	posY := reader.ReadUint16()
	timing := reader.ReadByte()
	_ = reader.ReadByte() // isMainTarget
	_ = reader.ReadByte()
	_ = reader.ReadByte()
	_ = reader.ReadByte()
	targetNum := reader.ReadByte()
	if targetNum == 0 && reader.Size == userSkillTargetMinSize {
		// This client reserves the target-count byte but sends zero for a
		// single user target. The five bytes after it are still the target.
		targetNum = 1
	}

	if slotValue < 0 || slotValue > int32(^uint16(0)) {
		log.Warningf("[SKILLTOUSER] invalid skill slot: skill=%d slot=%d", skillID, slotValue)
		return
	}
	slot := uint16(slotValue)

	if isMoving != 0 || targetNum == 0 || targetNum > maxPVPUserSkillTargets {
		log.Warningf("[SKILLTOUSER] invalid targeted skill: skill=%d slot=%d moving=%d targets=%d",
			skillID, slot, isMoving, targetNum)
		return
	}

	expectedSize := userSkillTargetMinSize + (int(targetNum)-1)*5
	if int(reader.Size) != expectedSize {
		log.Warningf("[SKILLTOUSER] invalid targeted skill size: skill=%d targets=%d size=%d expected=%d",
			skillID, targetNum, reader.Size, expectedSize)
		return
	}

	targets := make([]pvpUserSkillTarget, targetNum)
	for i := range targets {
		targets[i] = pvpUserSkillTarget{
			userIndex: decodeUserObjectIndex(reader.ReadInt32()),
			hitNum:    reader.ReadByte(),
		}
	}
	if len(targets) != 1 {
		log.Warningf("[SKILLTOUSER] personal PvP supports one target: skill=%d targets=%d", skillID, targetNum)
		return
	}

	targetUserIndex := targets[0].userIndex
	if targetUserIndex < 0 || targetUserIndex > int32(^uint16(0)) {
		log.Warningf("[SKILLTOUSER] invalid target user: skill=%d targetUser=%d", skillID, targetUserIndex)
		return
	}

	// Some clients send their own user index in the targeted skill packet when
	// the opponent has user index 0. During an active personal PvP session the
	// server-side opponent state is authoritative, so use it to repair this
	// directional index mismatch before resolving the target session.
	if targetUserIndex == int32(session.UserIdx) {
		attackerContext, err := context.Parse(session)
		if err != nil {
			log.Error(err.Error())
			return
		}

		attackerContext.Mutex.RLock()
		pvpType := attackerContext.PVP.Type
		opponentUserIndex := attackerContext.PVP.OpponentUserIdx
		attackerContext.Mutex.RUnlock()

		if pvpType != pvpTypePerson || opponentUserIndex == session.UserIdx {
			log.Warningf("[SKILLTOUSER] invalid target user: skill=%d targetUser=%d", skillID, targetUserIndex)
			return
		}

		log.Debugf("[SKILLTOUSER] corrected target user: attackerUser=%d packetTarget=%d pvpTarget=%d",
			session.UserIdx, targetUserIndex, opponentUserIndex)
		targetUserIndex = int32(opponentUserIndex)
	}

	targetSession := g_NetworkManager.GetSession(uint16(targetUserIndex))
	if targetSession == nil {
		log.Warningf("[SKILLTOUSER] target session not found: skill=%d targetUser=%d", skillID, targetUserIndex)
		return
	}

	attackerContext, targetContext, unlock, ok := lockPVPCombatContexts(session, targetSession)
	if !ok {
		return
	}
	targetCharacterID := int32(0)
	if targetContext.Char != nil {
		targetCharacterID = targetContext.Char.Id
	}
	if !validPVPCombatPair(session, targetSession, attackerContext, targetContext, targetCharacterID) {
		unlock()
		log.Warningf("[SKILLTOUSER] rejected PvP skill: attacker=%d targetUser=%d skill=%d",
			session.UserIdx, targetUserIndex, skillID)
		return
	}

	attacker := attackerContext.Char
	target := targetContext.Char
	attackerID := attacker.Id
	targetID := target.Id
	skillLevel := byte(1)
	for _, skill := range attacker.Skills.List {
		if skill.Id == skillID && skill.Level > 0 {
			skillLevel = skill.Level
			break
		}
	}
	unlock()

	skillMeta, hasSkillMeta := clientdata.FindSkillMeta(skillID)
	remainingMP, manaCost, manaChanged, manaEnough := consumeSkillMana(attackerContext, skillMeta, skillLevel)
	if hasSkillMeta && !manaEnough {
		log.Warningf("[SKILLTOUSER] insufficient MP: character=%d skill=%d level=%d current=%d required=%d",
			attackerID, skillID, skillLevel, remainingMP, manaCost)
		sendLastErrorCode(session, SKILLTOUSER, uint16(skillMeta.Group), battleModeErrorMPInsufficiency)
		return
	}
	if manaChanged {
		saveContextVitals(attackerContext)
	}

	attackerContext, targetContext, unlock, ok = lockPVPCombatContexts(session, targetSession)
	if !ok || !validPVPCombatPair(session, targetSession, attackerContext, targetContext, targetID) {
		if ok {
			unlock()
		}
		return
	}

	attacker = attackerContext.Char
	target = targetContext.Char
	if target.CurrentHP == 0 {
		currentHP, currentMP, currentSP := attacker.CurrentHP, attacker.CurrentMP, attacker.CurrentSP
		unlock()
		sendTargetedUserSkillResult(session, skillID, currentHP, currentMP, currentSP,
			[]pvpUserSkillResult{{charID: target.Id, result: attackResultDead, hitNum: targets[0].hitNum}})
		return
	}

	attack := calculateCharacterAttackFromCharacter(attacker)
	damage := attack.PhysicalMax
	if skillInfo, ok := clientdata.FindSkillDamage(skillID); ok {
		damage = skillInfo.Calculate(attack.PhysicalMax, attack.MagicAttack, skillLevel)
	}
	if damage > int(target.CurrentHP) {
		damage = int(target.CurrentHP)
	}
	target.CurrentHP -= uint16(damage)
	targetHP := target.CurrentHP
	currentHP, currentMP, currentSP := attacker.CurrentHP, attacker.CurrentMP, attacker.CurrentSP
	attackerID = attacker.Id
	targetID = target.Id
	attackerWorld := attackerContext.World
	result := pvpUserSkillResult{
		charID: targetID,
		result: attackResultNormal,
		damage: uint16(damage),
		hpRest: targetHP,
		hitNum: targets[0].hitNum,
	}
	unlock()

	if !saveContextVitals(targetContext) {
		log.Warningf("[SKILLTOUSER] unable to save target vitals: char=%d", targetID)
	}
	targetSession.Send(newSkillToMton(skillID, uint16(damage), skillLevel))
	session.Send(newTargetedUserSkillResult(skillID, currentHP, currentMP, currentSP, result))
	if attackerWorld != nil {
		attackerWorld.BroadcastSessionPacket(session, newTargetedUserSkillNotification(
			skillID, attackerID, posX, posY, result,
		))
	}

	log.Debugf("[SKILLTOUSER] attacker=%d target=%d skill=%d level=%d timing=%d damage=%d hp=%d",
		attackerID, targetID, skillID, skillLevel, timing, damage, targetHP)
}

type pvpUserSkillResult struct {
	charID int32
	result byte
	damage uint16
	hpRest uint16
	hitNum byte
}

func lockPVPCombatContexts(attackerSession, targetSession *network.Session) (
	*context.Context, *context.Context, func(), bool,
) {
	attackerContext, err := context.Parse(attackerSession)
	if err != nil {
		log.Error(err.Error())
		return nil, nil, nil, false
	}
	targetContext, err := context.Parse(targetSession)
	if err != nil {
		log.Error(err.Error())
		return nil, nil, nil, false
	}

	if attackerSession.UserIdx < targetSession.UserIdx {
		attackerContext.Mutex.Lock()
		targetContext.Mutex.Lock()
		return attackerContext, targetContext, func() {
			targetContext.Mutex.Unlock()
			attackerContext.Mutex.Unlock()
		}, true
	}

	targetContext.Mutex.Lock()
	attackerContext.Mutex.Lock()
	return attackerContext, targetContext, func() {
		attackerContext.Mutex.Unlock()
		targetContext.Mutex.Unlock()
	}, true
}

func validPVPCombatPair(attackerSession, targetSession *network.Session,
	attackerContext, targetContext *context.Context, targetCharacterID int32,
) bool {
	if attackerContext.Char == nil || targetContext.Char == nil || attackerContext.World == nil || targetContext.World == nil {
		return false
	}
	return attackerContext.World.GetId() == targetContext.World.GetId() &&
		attackerContext.PVP.Type == pvpTypePerson &&
		attackerContext.PVP.OpponentUserIdx == targetSession.UserIdx &&
		attackerContext.PVP.OpponentCharacterID == targetCharacterID &&
		targetContext.PVP.Type == pvpTypePerson &&
		targetContext.PVP.OpponentUserIdx == attackerSession.UserIdx &&
		targetContext.PVP.OpponentCharacterID == attackerContext.Char.Id &&
		targetContext.Char.Id == targetCharacterID
}

func sendPVPAttackResult(session *network.Session, characterID int32, hp, mp, sp uint16,
	result byte, damage, hpRest uint16,
) {
	session.Send(newPVPAttackResult(characterID, hp, mp, sp, result, damage, hpRest))
}

func newPVPAttackResult(characterID int32, hp, mp, sp uint16, result byte, damage, hpRest uint16) *network.Writer {
	pkt := network.NewWriter(ATTCKTOUSER)
	pkt.WriteInt32(characterID)
	pkt.WriteUint16(hp)
	pkt.WriteUint16(mp)
	pkt.WriteUint16(sp)
	pkt.WriteByte(result)
	pkt.WriteUint16(damage)
	pkt.WriteUint16(hpRest)
	return pkt
}

func newPVPAttackMotion(damage uint16) *network.Writer {
	pkt := network.NewWriter(NFY_ATTCKMOTION)
	pkt.WriteUint16(damage)
	return pkt
}

func newPVPAttackNotification(attackerID, targetID int32, result byte, hpRest uint16) *network.Writer {
	pkt := network.NewWriter(NFY_ATTCKTOUSER)
	pkt.WriteInt32(attackerID)
	pkt.WriteInt32(targetID)
	pkt.WriteByte(result)
	pkt.WriteUint16(hpRest)
	return pkt
}

func newSkillToMton(skillID, damage uint16, skillLevel byte) *network.Writer {
	pkt := network.NewWriter(NFY_SKILLTOMTON)
	pkt.WriteUint16(skillID)
	// The RU client expects applyAbilityKind between the skill id and the
	// damage fields. Without this reserved DWORD it reads past the short
	// packet while processing NFY_SKILLTOMTON and crashes.
	pkt.WriteUint32(0) // applyAbilityKind
	pkt.WriteUint16(damage)
	pkt.WriteByte(skillLevel)
	return pkt
}

func sendTargetedUserSkillResult(session *network.Session, skillID uint16, hp, mp, sp uint16, results []pvpUserSkillResult) {
	session.Send(newTargetedUserSkillResult(skillID, hp, mp, sp, results...))
}

func newTargetedUserSkillResult(skillID uint16, hp, mp, sp uint16, results ...pvpUserSkillResult) *network.Writer {
	pkt := network.NewWriter(SKILLTOUSER)
	pkt.WriteUint16(skillID)
	pkt.WriteUint16(hp)
	pkt.WriteUint16(mp)
	pkt.WriteUint16(sp)
	// These fields are part of the RU client's S2C_STUSKILL payload. They
	// must be present before targetNum even when no ability effect is active.
	pkt.WriteUint32(0) // applyAbilityKind
	pkt.WriteUint32(0) // remainedDamageAmp
	pkt.WriteByte(byte(len(results)))
	for _, result := range results {
		pkt.WriteInt32(result.charID)
		pkt.WriteByte(result.result)
		pkt.WriteUint16(result.damage)
		pkt.WriteUint16(result.hpRest)
		pkt.WriteByte(0)  // existBFX
		pkt.WriteInt32(0) // stunTime
		pkt.WriteByte(result.hitNum)
	}
	return pkt
}

func newTargetedUserSkillNotification(skillID uint16, attackerID int32, posX, posY uint16, result pvpUserSkillResult) *network.Writer {
	pkt := network.NewWriter(NFY_SKILLTOUSER)
	pkt.WriteUint16(skillID)
	pkt.WriteByte(1)
	pkt.WriteInt32(attackerID)
	pkt.WriteUint16(posX)
	pkt.WriteUint16(posY)
	pkt.WriteInt32(result.charID)
	pkt.WriteByte(result.result)
	pkt.WriteUint16(result.hpRest)
	pkt.WriteByte(0)  // existBFX
	pkt.WriteInt32(0) // stunTime
	pkt.WriteByte(result.hitNum)
	return pkt
}
