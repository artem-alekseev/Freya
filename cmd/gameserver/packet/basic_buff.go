package packet

import (
	"time"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	basicBuffOuterHeaderSize = 10
	basicBuffInnerHeaderSize = 10
	basicBuffPrefixSize      = basicBuffOuterHeaderSize + 2 + 2 + 1 + basicBuffInnerHeaderSize + 1 + 1
	basicBuffTargetSize      = 4 + 1

	buffTargetUser byte = 0x01

	buffResultOK      int32 = 0x00000001
	buffTargetInvalid int32 = 0x00200000
	buffTargetNull    int32 = 0x00400000
	buffDataError     int32 = 0x00002000
)

type basicBuffTarget struct {
	Index  int32
	Type   byte
	Result int32
}

type resolvedBuffTarget struct {
	requestIndex int32
	requestType  byte
	session      *network.Session
	context      *context.Context
	characterID  int32
}

func handleBasicBuff(session *network.Session, reader *network.Reader) {
	if int(reader.Size) < basicBuffPrefixSize {
		log.Warningf("[SKILLTOTARGET] invalid packet size: %d", reader.Size)
		return
	}

	buffType := reader.ReadUint16()
	skillID := reader.ReadUint16()
	slot := reader.ReadByte()
	// C2S_BASICBUFFF is embedded in bData and has its own C2S_HEADER.
	// WorldSvr casts bData directly to C2S_BASICBUFFF, so skip that
	// nested header before reading buffKind and targetCount.
	_ = reader.ReadBytes(basicBuffInnerHeaderSize)
	buffKind := reader.ReadByte()
	targetCount := reader.ReadByte()
	log.Debugf("[BASICBUFF] request: type=%d skill=%d slot=%d kind=%d targets=%d",
		buffType, skillID, slot, buffKind, targetCount)

	if targetCount == 0 || targetCount > 50 {
		log.Warningf("[SKILLTOTARGET] invalid target count: skill=%d count=%d", skillID, targetCount)
		return
	}

	expectedSize := basicBuffPrefixSize + int(targetCount)*basicBuffTargetSize
	if int(reader.Size) < expectedSize {
		log.Warningf("[SKILLTOTARGET] invalid target payload: skill=%d size=%d expected=%d",
			skillID, reader.Size, expectedSize)
		return
	}

	targets := make([]basicBuffTarget, targetCount)
	for index := range targets {
		targets[index] = basicBuffTarget{
			Index: reader.ReadInt32(),
			Type:  reader.ReadByte(),
		}
		log.Debugf("[BASICBUFF] target[%d]: index=%d type=%d",
			index, targets[index].Index, targets[index].Type)
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	level, learned := learnedBuffLevel(ctx, skillID, slot)
	if !learned {
		log.Warningf("[BASICBUFF] skill is not learned: character=%d skill=%d slot=%d",
			characterID(ctx), skillID, slot)
		return
	}

	buffInfo, hasBuffInfo := clientdata.FindSkillBuff(skillID)
	if !hasBuffInfo {
		log.Warningf("[BASICBUFF] no client buff data: character=%d skill=%d",
			characterID(ctx), skillID)
		sendBasicBuffResult(session, skillID, level, buffKind, targets, 0, currentMana(ctx))
		return
	}

	resolved := make([]resolvedBuffTarget, 0, len(targets))
	results := make([]basicBuffTarget, len(targets))
	copy(results, targets)
	seen := make(map[int32]struct{}, len(targets))
	for index, target := range targets {
		if target.Type != buffTargetUser {
			results[index].Result = buffTargetInvalid
			continue
		}

		targetSession, targetContext, targetCharacterID := findBuffTarget(ctx, target.Index)
		if targetSession == nil || targetContext == nil {
			results[index].Result = buffTargetNull
			continue
		}
		if _, duplicate := seen[targetCharacterID]; duplicate {
			results[index].Result = buffResultOK
			continue
		}
		seen[targetCharacterID] = struct{}{}
		resolved = append(resolved, resolvedBuffTarget{
			requestIndex: target.Index,
			requestType:  target.Type,
			session:      targetSession,
			context:      targetContext,
			characterID:  targetCharacterID,
		})
	}

	if len(resolved) == 0 {
		log.Warningf("[BASICBUFF] no valid targets: character=%d skill=%d",
			characterID(ctx), skillID)
		sendBasicBuffResult(session, skillID, level, buffKind, results, 0, currentMana(ctx))
		return
	}

	meta, hasMeta := clientdata.FindSkillMeta(skillID)
	if !hasMeta {
		log.Warningf("[BASICBUFF] no client skill metadata: character=%d skill=%d",
			characterID(ctx), skillID)
		return
	}
	remainingMP, _, manaChanged, manaEnough := consumeSkillMana(ctx, meta, level)
	if !manaEnough {
		log.Warningf("[BASICBUFF] insufficient MP: character=%d skill=%d current=%d",
			characterID(ctx), skillID, remainingMP)
		sendLastErrorCode(session, SKILLTOMOBS, uint16(meta.Group), battleModeErrorMPInsufficiency)
		return
	}
	if manaChanged {
		saveContextVitals(ctx)
	}

	effects := make([]context.BuffEffect, 0, len(buffInfo.Effects))
	for _, effect := range buffInfo.Effects {
		effects = append(effects, context.BuffEffect{
			ForceID:   effect.ForceID,
			Value:     effect.Value(level),
			ValueType: effect.ValueType,
		})
	}
	durationMS := buffInfo.Duration(level)
	duration := time.Duration(durationMS) * time.Millisecond

	skillExperience := skillExperienceResult{}
	if skillInfo, ok := clientdata.FindSkillDamage(skillID); ok {
		skillExperience = addSkillExperience(ctx, skillInfo.Type,
			uint32(skillInfo.Experience())*skillExperienceMultiplier)
	}

	var sourceCharacterID int32
	ctx.Mutex.RLock()
	sourceCharacterID = ctx.Char.Id
	ctx.Mutex.RUnlock()

	for _, target := range resolved {
		generation := target.context.ApplyBuff(skillID, level, buffType, buffKind, effects, duration)
		for index := range results {
			if results[index].Index == target.requestIndex && results[index].Type == target.requestType && results[index].Result == 0 {
				results[index].Index = target.characterID
				results[index].Result = buffResultOK
				break
			}
		}
		if duration > 0 {
			targetSession := target.session
			targetGeneration := generation
			targetEffects := append([]clientdata.SkillBuffEffect(nil), buffInfo.Effects...)
			time.AfterFunc(duration, func() {
				expireBasicBuff(targetSession, targetGeneration, skillID, buffType, targetEffects)
			})
		}
	}

	sendBasicBuffResult(session, skillID, level, buffKind, results,
		skillExperience.Applied, currentMana(ctx))
	notification := newBasicBuffNotification(sourceCharacterID, skillID, level, buffKind, results)
	ctx.World.BroadcastSessionPacket(session, notification)
	log.Debugf("[BASICBUFF] character=%d skill=%d level=%d targets=%d duration=%dms",
		sourceCharacterID, skillID, level, len(resolved), durationMS)

	if skillExperience.SwordRankUp {
		sendSkillRankUpdate(session, 1, skillExperience.SwordRank)
		sendClassRankUpEvent(session, sourceCharacterID)
	}
	if skillExperience.MagicRankUp {
		sendSkillRankUpdate(session, 2, skillExperience.MagicRank)
		sendClassRankUpEvent(session, sourceCharacterID)
	}
	if skillExperience.SwordRankUp || skillExperience.MagicRankUp {
		sendHealthUpdate(session, skillExperience.CurrentHP)
		sendManaUpdate(session, skillExperience.CurrentMP)
	}
}

func learnedBuffLevel(ctx *context.Context, skillID uint16, slot byte) (byte, bool) {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	if ctx.Char == nil {
		return 0, false
	}
	skill := ctx.Char.Skills.Get(uint16(slot))
	if skill.Id == skillID && skill.Level > 0 {
		return skill.Level, true
	}
	return 0, false
}

func findBuffTarget(sourceContext *context.Context, index int32) (*network.Session, *context.Context, int32) {
	const userIndexMask uint32 = 0x0000FFFF
	userIndex := int32(uint32(index) & userIndexMask)

	worldID := byte(0)
	sourceContext.Mutex.RLock()
	if sourceContext.World != nil {
		worldID = sourceContext.World.GetId()
	}
	sourceContext.Mutex.RUnlock()

	// WorldSvr resolves OT_USER through MASK_USERINDEX. Prefer the direct
	// session lookup for that value; the character-id scan below is kept for
	// clients that send the character id instead.
	candidate := g_NetworkManager.GetSession(uint16(userIndex))
	if candidate != nil {
		candidateContext, err := context.Parse(candidate)
		if err == nil {
			candidateContext.Mutex.RLock()
			if candidateContext.Char != nil && candidateContext.World != nil && candidateContext.World.GetId() == worldID {
				characterID := candidateContext.Char.Id
				candidateContext.Mutex.RUnlock()
				return candidate, candidateContext, characterID
			}
			candidateContext.Mutex.RUnlock()
		}
	}

	var fallback *network.Session
	for _, candidate := range g_NetworkManager.Sessions() {
		candidateContext, err := context.Parse(candidate)
		if err != nil {
			continue
		}
		candidateContext.Mutex.RLock()
		if candidateContext.Char == nil || candidateContext.World == nil || candidateContext.World.GetId() != worldID {
			candidateContext.Mutex.RUnlock()
			continue
		}
		candidateCharacterID := candidateContext.Char.Id
		candidateContext.Mutex.RUnlock()

		if candidateCharacterID == index || candidateCharacterID == userIndex {
			return candidate, candidateContext, candidateCharacterID
		}
		if candidate.UserIdx != uint16(userIndex) {
			continue
		}
		fallback = candidate
	}

	if fallback == nil {
		return nil, nil, 0
	}
	fallbackContext, err := context.Parse(fallback)
	if err != nil {
		return nil, nil, 0
	}
	fallbackContext.Mutex.RLock()
	if fallbackContext.Char == nil {
		fallbackContext.Mutex.RUnlock()
		return nil, nil, 0
	}
	characterID := fallbackContext.Char.Id
	fallbackContext.Mutex.RUnlock()
	return fallback, fallbackContext, characterID
}

func sendBasicBuffResult(session *network.Session, skillID uint16, level byte, buffKind byte, targets []basicBuffTarget, skillExp uint16, currentMP uint16) {
	pkt := network.NewWriter(BASICBUFF)
	pkt.WriteUint16(skillID)
	pkt.WriteUint16(uint16(level))
	pkt.WriteByte(buffKind)
	pkt.WriteByte(byte(len(targets)))
	pkt.WriteUint32(uint32(skillExp)) // skill EXP
	pkt.WriteUint16(currentMP)
	for _, target := range targets {
		pkt.WriteInt32(target.Index)
		pkt.WriteByte(target.Type)
		pkt.WriteInt32(target.Result)
	}
	writeWorldSvrBasicBuffTail(pkt)
	session.Send(pkt)
}

func newBasicBuffNotification(attackerID int32, skillID uint16, level byte, buffKind byte, targets []basicBuffTarget) *network.Writer {
	pkt := network.NewWriter(NFY_BASICBUFF)
	pkt.WriteInt32(attackerID)
	pkt.WriteUint16(skillID)
	pkt.WriteUint16(uint16(level))
	pkt.WriteByte(buffKind)
	pkt.WriteByte(byte(len(targets)))
	for _, target := range targets {
		pkt.WriteInt32(target.Index)
		pkt.WriteByte(target.Type)
		pkt.WriteInt32(target.Result)
	}
	writeWorldSvrBasicBuffTail(pkt)
	return pkt
}

// WorldSvr calculates variable basic-buff packets from a packed struct that
// already contains one TargetDataSend entry and then adds one BYTE plus one
// TargetDataSend entry to the payload length. The extra ten bytes are zeroed
// in its stack buffer and are part of the packet sent to the client.
func writeWorldSvrBasicBuffTail(pkt *network.Writer) {
	pkt.WriteBytes(make([]byte, 1+4+1+4))
}

func expireBasicBuff(session *network.Session, generation uint64, skillID, buffType uint16, effects []clientdata.SkillBuffEffect) {
	ctx, err := context.Parse(session)
	if err != nil || !ctx.ExpireBuff(generation) {
		return
	}

	ctx.Mutex.RLock()
	characterID := ctx.Char.Id
	world := ctx.World
	ctx.Mutex.RUnlock()

	forceID := int32(0)
	if len(effects) > 0 {
		forceID = int32(effects[0].ForceID)
	}
	log.Debugf("[BUFF] expired: character=%d skill=%d type=%d force=%d",
		characterID, skillID, buffType, forceID)
	if world == nil {
		return
	}

	pkt := network.NewWriter(NFY_BUFFFEND)
	pkt.WriteInt32(characterID)
	pkt.WriteByte(byte(buffType))
	pkt.WriteUint16(skillID)
	pkt.WriteInt32(forceID)
	world.BroadcastSessionPacket(session, pkt)
}

func characterID(ctx *context.Context) int32 {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()
	if ctx.Char == nil {
		return 0
	}
	return ctx.Char.Id
}

func currentMana(ctx *context.Context) uint16 {
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()
	if ctx.Char == nil {
		return 0
	}
	return ctx.Char.CurrentMP
}
