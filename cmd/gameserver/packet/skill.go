package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/network"
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
		Index   int
		Type    byte
		HitNum  byte
		MissMob byte
		MobHp   int
	}, targetNum)

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	id := ctx.Char.Id
	ctx.Mutex.RUnlock()

	damage := 10

	for i := 0; i < int(targetNum); i++ {
		targets[i].Index = int(reader.ReadInt32())
		targets[i].Type = reader.ReadByte()
		targets[i].HitNum = reader.ReadByte()
		targets[i].MissMob = reader.ReadByte()

		mob := ctx.World.FindMob(targets[i].Index)
		mob.SubHealth(damage)
		targets[i].MobHp, _ = mob.GetHealth()
	}

	//log.Debugf(
	//	"SkillToMob: skillID=%d, slot=%d, isMoving=%d, pos=(%d,%d), timing=%d, mainTarget=%d, targets=%d",
	//	wSkillIdx, bSlotIdx, isMoving, posX, posY, timing, isMainTarget, targetNum,
	//)
	//
	//log.Debug(targets)

	pkt := network.NewWriter(SKILLTOMOBS)
	// 1. Заголовок и ID скилла
	pkt.WriteUint16(wSkillIdx)

	pkt.WriteUint16(39)   // HP +
	pkt.WriteUint16(29)   // MP +
	pkt.WriteUint16(5000) // SP +

	pkt.WriteUint32(0) // Soul Ability EXP
	pkt.WriteUint32(0) // applyAbilityKind

	pkt.WriteUint32(100) // Skill EXP +

	pkt.WriteUint64(100) // EXP
	pkt.WriteUint16(0)   // Soul Ability Points

	pkt.WriteUint32(0) //amp

	// 3. Количество целей
	pkt.WriteByte(uint8(len(targets)))

	// 4. Данные по каждой цели
	for _, target := range targets {
		pkt.WriteInt32(target.Index) // index
		pkt.WriteByte(2)             // type +
		pkt.WriteByte(1)             // result +
		pkt.WriteInt16(damage)       //damage
		pkt.WriteInt32(target.MobHp) //mobhp
		pkt.WriteUint16(0)           // Позиция X (если BFX_MOVE)
		pkt.WriteUint16(0)           // Позиция Y (если BFX_MOVE)
		pkt.WriteByte(0)             // Эффект (BFX_STUN, BFX_MOVE...)
		pkt.WriteUint32(0)           // Длительность стана (если BFX_STUN)
		pkt.WriteByte(target.HitNum) // Количество хитов
	}
	session.Send(pkt)

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
		npkt.WriteInt32(2)            // type
		npkt.WriteByte(2)             // result
		npkt.WriteInt32(target.MobHp)
		npkt.WriteByte(2)
		npkt.WriteByte(0)
		npkt.WriteInt16(0)
		npkt.WriteInt16(0)
		npkt.WriteInt32(0)
	}

	ctx.World.BroadcastSessionPacket(session, npkt)
}
