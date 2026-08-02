package packet

import (
	"fmt"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
)

const (
	attackToMobsPacketSize = 16
	attackResultNormal     = 2
	attackObjectMobs       = 2
)

// AttckToMobs handles CSC_ATTCKTOMOBS, the client packet emitted by a normal
// attack. Its body is iToIdx, object type and the missMob flag.
func AttckToMobs(session *network.Session, reader *network.Reader) {
	if reader.Size != attackToMobsPacketSize {
		log.Warningf("Ignoring malformed AttckToMobs packet: size=%d", reader.Size)
		return
	}

	mobID := reader.ReadInt32()
	objectType := reader.ReadByte()
	_ = reader.ReadByte() // missMob
	if objectType != attackObjectMobs {
		log.Warningf("Ignoring AttckToMobs for unsupported object type: mob=%d type=%d", mobID, objectType)
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	world := ctx.World
	currentHP := ctx.Char.CurrentHP
	currentMP := ctx.Char.CurrentMP
	currentSP := ctx.Char.CurrentSP
	ctx.Mutex.RUnlock()

	if world == nil {
		log.Warningf("Ignoring AttckToMobs for character without a world: mob=%d", mobID)
		return
	}

	mob := world.FindMob(int(mobID))
	if mob == nil {
		log.Warningf("Ignoring AttckToMobs for unknown mob: mob=%d", mobID)
		return
	}

	previousMobHP, _ := mob.GetHealth()
	damage := 0
	var gainedExperience uint64
	spiritChanged := false
	if previousMobHP > 0 {
		attack := calculateCharacterAttack(ctx)
		sendCharacterAttackValue(session, attack)
		damage = attack.PhysicalMax
		if damage > previousMobHP {
			damage = previousMobHP
		}

		if mob.SubHealth(damage) {
			gainedExperience = mob.GetExperience()
		}
		spiritChanged = addSpiritPoints(ctx, 1)
	}

	mobHP, _ := mob.GetHealth()
	previousLevel, currentLevel, experienceUpdated, classRankUp := addExperience(ctx, gainedExperience)

	ctx.Mutex.RLock()
	currentHP = ctx.Char.CurrentHP
	currentMP = ctx.Char.CurrentMP
	currentSP = ctx.Char.CurrentSP
	experience := ctx.Char.Exp
	characterID := ctx.Char.Id
	ctx.Mutex.RUnlock()
	if spiritChanged {
		saveContextVitals(ctx)
	}

	session.Send(newAttackToMobsResult(
		mobID,
		objectType,
		currentHP,
		currentMP,
		currentSP,
		byte(attackResultNormal),
		experience,
		0,
		0,
		uint16(damage),
		int32(mobHP),
	))

	ctx.World.BroadcastSessionPacket(session, newAttackToMobsNotification(
		characterID,
		mobID,
		objectType,
		byte(attackResultNormal),
		int32(mobHP),
	))

	if experienceUpdated {
		if currentLevel > previousLevel {
			if err := ensureBattleModeSkillsForContext(ctx); err != nil {
				log.Errorf("Unable to grant battle mode skills for character %d: %s", characterID, err)
			}
		}
		sendExperienceUpdate(session, experience)
		if currentLevel > previousLevel {
			sendLevelUpdate(session, currentLevel)
			sendHealthUpdate(session, currentHP)
			sendManaUpdate(session, currentMP)
			sendLevelUpEvent(session, characterID)
		}
		if classRankUp {
			sendClassRankUpdate(session, character.ClassRankForLevel(currentLevel))
			sendClassRankUpEvent(session, characterID)
		}
	}
}

func calculateCharacterAttack(ctx *context.Context) character.AttackStats {
	ctx.Mutex.RLock()
	if ctx.Char == nil {
		ctx.Mutex.RUnlock()
		return character.AttackStats{}
	}
	copyCharacter := *ctx.Char
	ctx.Mutex.RUnlock()

	copyCharacter.STR = addBuffToStat(copyCharacter.STR,
		ctx.BuffValue(45)+ctx.BuffValue(48))
	copyCharacter.INT = addBuffToStat(copyCharacter.INT,
		ctx.BuffValue(46)+ctx.BuffValue(48))
	copyCharacter.DEX = addBuffToStat(copyCharacter.DEX,
		ctx.BuffValue(47)+ctx.BuffValue(48))

	attack := calculateCharacterAttackFromCharacter(&copyCharacter)
	attack.PhysicalMax += ctx.BuffValue(3) + ctx.BuffValue(23) +
		ctx.BuffValue(113) + ctx.BuffValue(115)
	attack.MagicAttack += ctx.BuffValue(4) + ctx.BuffValue(24) +
		ctx.BuffValue(113) + ctx.BuffValue(115)
	if attack.PhysicalMax < 1 {
		attack.PhysicalMax = 1
	}
	if attack.MagicAttack < 1 {
		attack.MagicAttack = 1
	}
	attack.PhysicalMin = attack.PhysicalMax*4/5 + attack.PhysicalMax/20
	if attack.PhysicalMin > attack.PhysicalMax {
		attack.PhysicalMin = attack.PhysicalMax
	}
	return attack
}

func addBuffToStat(value uint32, delta int) uint32 {
	if delta >= 0 {
		if uint64(value)+uint64(delta) > uint64(^uint32(0)) {
			return ^uint32(0)
		}
		return value + uint32(delta)
	}
	decrease := uint32(-delta)
	if decrease > value {
		return 0
	}
	return value - decrease
}

func calculateCharacterAttackFromCharacter(c *character.Character) character.AttackStats {
	if c == nil {
		return character.AttackStats{}
	}

	attack := c.CalculateAttack()
	attack.PhysicalMax += weaponAttack(c.Equipment.Get(uint16(inventory.RightHand)))
	attack.PhysicalMax += weaponAttack(c.Equipment.Get(uint16(inventory.LeftHand)))
	attack.MagicAttack += weaponMagicAttack(c.Equipment.Get(uint16(inventory.RightHand)))
	attack.MagicAttack += weaponMagicAttack(c.Equipment.Get(uint16(inventory.LeftHand)))
	attack.PhysicalMin = attack.PhysicalMax*4/5 + attack.PhysicalMax/20

	return attack
}

func weaponAttack(item inventory.Item) int {
	attack, ok := clientdata.FindWeaponAttack(item.Kind)
	if !ok {
		return 0
	}

	return attack
}

func weaponMagicAttack(item inventory.Item) int {
	attack, ok := clientdata.FindWeaponMagicAttack(item.Kind)
	if !ok {
		return 0
	}

	return attack
}

func sendCharacterAttackValue(session *network.Session, attack character.AttackStats) {
	session.Send(SendMessage(session, fmt.Sprintf("Attack: %d Magic attack: %d", attack.PhysicalMax, attack.MagicAttack)))
}

func newAttackToMobsResult(
	mobID int32,
	objectType byte,
	currentHP uint16,
	currentMP uint16,
	currentSP uint16,
	result byte,
	experience uint64,
	swordExperience int32,
	magicExperience int32,
	damage uint16,
	mobHP int32,
) *network.Writer {
	pkt := network.NewWriter(ATTCKTOMOBS)
	pkt.WriteInt32(mobID)
	pkt.WriteByte(objectType)
	pkt.WriteUint16(currentHP)
	pkt.WriteUint16(currentMP)
	pkt.WriteUint16(currentSP)
	pkt.WriteByte(result)
	pkt.WriteUint64(experience)
	pkt.WriteInt32(swordExperience)
	pkt.WriteInt32(magicExperience)
	pkt.WriteUint16(damage)
	pkt.WriteInt32(mobHP)

	return pkt
}

func newAttackToMobsNotification(
	characterID int32,
	mobID int32,
	objectType byte,
	result byte,
	mobHP int32,
) *network.Writer {
	pkt := network.NewWriter(NFY_ATTCKTOMOBS)
	pkt.WriteByte(result)
	pkt.WriteInt32(characterID)
	pkt.WriteInt32(mobID)
	pkt.WriteByte(objectType)
	pkt.WriteInt32(mobHP)

	return pkt
}
