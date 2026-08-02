package packet

import (
	"bytes"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/models/subpasswd"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

// NewTargetUser Packet
func NewTargetUser(session *network.Session, reader *network.Reader) {
	sessionId := reader.ReadUint16()

	pSession := g_NetworkManager.GetSession(sessionId)
	ctx, err := context.Parse(pSession)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	currentHP, maxHP := ctx.Char.CurrentHP, ctx.Char.MaxHP
	ctx.Mutex.RUnlock()

	var packet = network.NewWriter(NEW_TARGET_USER)

	packet.WriteByte(0x00)
	packet.WriteInt16(currentHP)
	packet.WriteInt16(maxHP)

	session.Send(packet)
}

func EndTargetUser(session *network.Session, reader *network.Reader) {
	sessionId := reader.ReadUint16()
	pSession := g_NetworkManager.GetSession(sessionId)
	ctx, err := context.Parse(pSession)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	//currentHP, maxHP := ctx.Char.CurrentHP, ctx.Char.MaxHP
	ctx.Mutex.RUnlock()
}

// GetMyChartr Packet
func GetMyChartr(session *network.Session, reader *network.Reader) {
	if !session.Data.Verified {
		log.Error("Unauthorized connection from", session.GetEndPnt())
		session.Close()
		return
	}

	// fetch subpassword
	var req = subpasswd.FetchReq{session.Data.AccountId}
	var res = subpasswd.FetchRes{}
	g_RPCHandler.Call(rpc.FetchSubPassword, req, &res)

	session.Data.SubPassword = &res.Details

	var subpasswdExist = 0
	if g_ServerConfig.IgnoreSubPassword || res.Password != "" {
		subpasswdExist = 1
	}

	// fetch characters
	var reqList = character.ListReq{session.Data.AccountId, byte(g_ServerSettings.ServerId)}
	var resList = character.ListRes{}
	g_RPCHandler.Call(rpc.LoadCharacters, reqList, &resList)

	session.Data.CharacterList = resList.List

	var packet = network.NewWriter(GETMYCHARTR)
	packet.WriteInt32(subpasswdExist)
	packet.WriteBytes(make([]byte, 10))
	packet.WriteInt32(resList.LastId)
	packet.WriteInt32(resList.SlotOrder)

	if len(resList.List) > 0 {
		for i := 0; i < len(resList.List); i++ {
			var character = resList.List[i]
			packet.WriteUint32(character.Id)
			packet.WriteInt64(character.Created.Unix())
			packet.WriteUint32(character.Style.Get())
			packet.WriteUint32(character.Level) //LVL
			packet.WriteByte(character.SwordRank)
			packet.WriteByte(character.MagicRank)
			packet.WriteUint16(0)
			packet.WriteUint64(character.Alz)
			packet.WriteByte(character.Nation)
			packet.WriteByte(character.World)
			packet.WriteUint16(character.X)
			packet.WriteUint16(character.Y)
			packet.WriteBytes(character.Equipment.SerializeKind())
			packet.WriteBytes(make([]byte, 4))
			packet.WriteByte(len(character.Name) + 1)
			packet.WriteString(character.Name + "\x00")
		}
	}

	session.Send(packet)
}

// NewMyChartr Packet
func NewMyChartr(session *network.Session, reader *network.Reader) {
	var style = reader.ReadUint32()
	var _ = reader.ReadByte() // beginner join guild
	var slot = reader.ReadByte()
	var nameLength = reader.ReadByte()
	var name = string(bytes.Trim(reader.ReadBytes(int(nameLength)), "\x00"))

	var charId = session.Data.AccountId*8 + int32(slot)
	var newStyle = character.Style{}
	newStyle.Set(style)

	var packet = network.NewWriter(NEWMYCHARTR)

	if !newStyle.Verify() || slot > 5 || nameLength > 16 {
		// invalid style, slot or nameLength
		packet.WriteInt32(0x00)
		packet.WriteByte(character.NowAllowed)

		session.Send(packet)
		return
	}

	// check if slot is used
	var charList = session.Data.CharacterList
	for i := 0; i < len(charList); i++ {
		if charList[i].Id == charId {
			packet.WriteInt32(0x00)
			packet.WriteByte(character.SlotInUse)

			session.Send(packet)
			return
		}
	}

	var req = character.CreateReq{
		byte(g_ServerSettings.ServerId),
		character.Character{Id: charId, Name: name, Style: newStyle},
	}
	var res = character.CreateRes{}
	g_RPCHandler.Call(rpc.CreateCharacter, req, &res)

	if res.Result == character.Success {
		packet.WriteInt32(charId)
		packet.WriteByte(res.Result + newStyle.BattleStyle)
		// update character with it's data
		session.Data.CharacterList = append(session.Data.CharacterList, res.Character)
	} else {
		packet.WriteInt32(0x00)
		packet.WriteByte(res.Result)
	}

	session.Send(packet)
}

// DelMyChartr Packet
func DelMyChartr(session *network.Session, reader *network.Reader) {
	var charId = reader.ReadInt32()

	// if password wasn't verified
	if !session.Data.CharVerified {
		return
	}

	// if subpasswd wasn't verified
	if len(session.Data.SubPassword.Password) > 0 && !session.Data.SubPassword.Verified {
		return
	}

	// verify character id
	if charId>>3 != session.Data.AccountId {
		return
	}

	var req = character.DeleteReq{byte(g_ServerSettings.ServerId), charId}
	var res = character.DeleteRes{}
	g_RPCHandler.Call(rpc.DeleteCharacter, req, &res)

	if res.Result == character.Success {
		// reset character delete passwd verification
		session.Data.CharVerified = false

		// reset character delete subpasswd verification
		session.Data.SubPassword.Verified = false

		var l = &session.Data.CharacterList

		// remove character from the list
		for key, value := range *l {
			if value.Id == charId {
				*l = append((*l)[:key], (*l)[key+1:]...)
				break
			}
		}
	}

	var packet = network.NewWriter(DELMYCHARTR)
	packet.WriteByte(res.Result + 1)
	packet.WriteByte(0x00)

	session.Send(packet)
}

// SetCharacterSlotOrder Packet
func SetCharacterSlotOrder(session *network.Session, reader *network.Reader) {
	var order = reader.ReadInt32()

	var req = character.SetOrderReq{
		byte(g_ServerSettings.ServerId),
		session.Data.AccountId,
		order,
	}
	var res = character.SetOrderRes{}
	g_RPCHandler.Call(rpc.SetSlotOrder, req, &res)

	var packet = network.NewWriter(SET_CHAR_SLOT_ORDER)
	packet.WriteByte(0x01)

	session.Send(packet)
}

func notifyChangeStyle(session *network.Session) {
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	id := ctx.Char.Id
	style := ctx.Char.Style
	liveStyle := ctx.Char.LiveStyle
	ctx.Mutex.RUnlock()

	pkt := network.NewWriter(NFY_CHANGESTYLE)
	pkt.WriteInt32(id)
	pkt.WriteInt32(style.Get())
	pkt.WriteInt32(liveStyle)
	pkt.WriteInt32(0)
	pkt.WriteInt16(0)

	ctx.World.BroadcastSessionPacket(session, pkt)
}

func ChangeStyle(session *network.Session, reader *network.Reader) {
	_ = reader.ReadInt32()          // style
	liveStyle := reader.ReadInt32() //liveStyle
	//guildNo := reader.ReadInt32()    // guildNo ?? 14 байт
	//guildColor := reader.ReadInt32() // guildColor
	//guildName := reader.ReadString(16) //guildName

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.Lock()
	ctx.Char.LiveStyle = liveStyle
	ctx.Mutex.Unlock()

	pkt := network.NewWriter(CHANGESTYLE)
	pkt.WriteByte(1)

	session.Send(pkt)

	notifyChangeStyle(session)
}

func SkillToActs(session *network.Session, reader *network.Reader) {
	target := reader.ReadInt32() // self char id
	action := reader.ReadUint16()
	x := reader.ReadByte()
	y := reader.ReadByte()

	id, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	pkt := network.NewWriter(NFY_SKILLTOACTS)
	pkt.WriteInt32(id)
	pkt.WriteInt32(target)
	pkt.WriteUint16(action)
	pkt.WriteByte(x)
	pkt.WriteByte(y)

	ctx.World.BroadcastSessionPacket(session, pkt)
}

func SkillToUser(session *network.Session, reader *network.Reader) {
	// seems like there are 2 types of messages
	switch reader.Size {
	case 15:
		// short self-target skills, including Battle Mode 1
		handleShortStyleSkill(session, reader)
	case 19:
		// astral & style related
		handleStyleSkill(session, reader)
	case 17:
		// dash/fade & movement related
		handleMoveSkill(session, reader)
	default:
		log.Warningf("Ignoring SkillToUser with unsupported size: %d", reader.Size)
	}
}

func handleShortStyleSkill(session *network.Session, reader *network.Reader) {
	skill := reader.ReadUint16()
	slot := uint16(reader.ReadByte())
	value := reader.ReadUint16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	if battleMode, handled := handleBattleModeSkill(ctx, skill, slot, value); handled {
		if !battleMode.Accepted {
			sendBattleModeFailure(session, battleMode)
			return
		}

		responseValue := battleMode.Start
		pkt := network.NewWriter(SKILLTOUSER)
		pkt.WriteUint16(skill)
		pkt.WriteUint16(battleMode.CurrentMP)
		pkt.WriteUint16(responseValue)
		session.Send(pkt)

		if battleMode.Save {
			saveContextVitals(ctx)
		}
		if battleMode.Start != 0 {
			startBattleModeDrain(session, ctx)
		} else {
			session.RemoveJob(battleModeDrainJob)
		}
		sendManaUpdate(session, battleMode.CurrentMP)
		sendSpiritUpdate(session, battleMode.CurrentSP)

		pkt = network.NewWriter(NFY_SKILLTOUSER)
		pkt.WriteUint16(skill)
		pkt.WriteInt32(battleMode.ID)
		pkt.WriteUint32(battleMode.Style)
		pkt.WriteByte(battleMode.LiveStyle)
		pkt.WriteByte(battleMode.StyleEx)
		pkt.WriteUint16(responseValue)
		ctx.World.BroadcastSessionPacket(session, pkt)
		return
	}

	ctx.Mutex.RLock()
	id := ctx.Char.Id
	mp := ctx.Char.CurrentMP
	style := ctx.Char.Style.Get()
	liveStyle := ctx.Char.LiveStyle
	ctx.Mutex.RUnlock()

	pkt := network.NewWriter(SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint16(mp)
	pkt.WriteUint16(value)
	session.Send(pkt)

	pkt = network.NewWriter(NFY_SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint32(id)
	pkt.WriteUint32(style)
	pkt.WriteByte(liveStyle)
	pkt.WriteByte(0x02)
	pkt.WriteUint16(value)

	ctx.World.BroadcastSessionPacket(session, pkt)
}

func SkillToTarget(session *network.Session, reader *network.Reader) {
	skill := reader.ReadUint16()
	_ = reader.ReadByte() // slot
	unk1 := reader.ReadInt16()
	unk2 := reader.ReadInt32()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	id := ctx.Char.Id
	mp := ctx.Char.CurrentMP
	style := ctx.Char.Style.Get()
	liveStyle := ctx.Char.LiveStyle
	ctx.Mutex.RUnlock()

	pkt := network.NewWriter(SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint16(mp)
	pkt.WriteInt16(unk1)
	pkt.WriteInt32(unk2)

	session.Send(pkt)

	pkt = network.NewWriter(NFY_SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint32(id)
	pkt.WriteUint32(style)
	pkt.WriteByte(liveStyle)
	pkt.WriteByte(0x02)
	pkt.WriteInt16(unk1)
	pkt.WriteInt32(unk2)

	ctx.World.BroadcastSessionPacket(session, pkt)
}

func handleStyleSkill(session *network.Session, reader *network.Reader) {
	skill := reader.ReadUint16()
	_ = reader.ReadByte() // slot
	unk1 := reader.ReadInt16()
	unk2 := reader.ReadInt32()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	id := ctx.Char.Id
	mp := ctx.Char.CurrentMP
	style := ctx.Char.Style.Get()
	liveStyle := ctx.Char.LiveStyle
	ctx.Mutex.RUnlock()

	pkt := network.NewWriter(SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint16(mp)
	pkt.WriteInt16(unk1)
	pkt.WriteInt32(unk2)

	session.Send(pkt)

	pkt = network.NewWriter(NFY_SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint32(id)
	pkt.WriteUint32(style)
	pkt.WriteByte(liveStyle)
	pkt.WriteByte(0x02)
	pkt.WriteInt16(unk1)
	pkt.WriteInt32(unk2)

	ctx.World.BroadcastSessionPacket(session, pkt)
}

func handleMoveSkill(session *network.Session, reader *network.Reader) {
	skill := reader.ReadUint16()
	_ = reader.ReadByte() // slot
	x := reader.ReadInt16()
	y := reader.ReadInt16()

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.Lock()
	id := ctx.Char.Id
	mp := ctx.Char.CurrentMP
	ctx.Char.X = byte(x)
	ctx.Char.Y = byte(y)
	ctx.Char.BeginX = x
	ctx.Char.BeginY = y
	ctx.Char.EndX = x
	ctx.Char.EndY = y
	ctx.Mutex.Unlock()

	pkt := network.NewWriter(SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteInt32(0)
	pkt.WriteUint16(mp)

	session.Send(pkt)

	pkt = network.NewWriter(NFY_SKILLTOUSER)
	pkt.WriteUint16(skill)
	pkt.WriteUint32(id)
	pkt.WriteUint16(session.UserIdx)
	pkt.WriteInt16(0x1000)
	pkt.WriteInt16(x)
	pkt.WriteInt16(y)

	ctx.World.BroadcastSessionPacket(session, pkt)
	ctx.World.AdjustCell(session)
}

func GetPlayerLevel(session *network.Session) int {
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return 0
	}

	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	return int(ctx.Char.Level)
}

func SetPlayerLevel(session *network.Session, level int) {
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.Lock()
	previousClassRank := ctx.Char.Style.MasteryLevel
	ctx.Char.Level = uint16(level)
	currentClassRank := character.ClassRankForLevel(ctx.Char.Level)
	ctx.Char.Style.MasteryLevel = currentClassRank
	id := ctx.Char.Id
	ctx.Mutex.Unlock()
	if err := ensureBattleModeSkillsForContext(ctx); err != nil {
		log.Errorf("Unable to grant battle mode skills for character %d: %s", id, err)
	}

	pkt := network.NewWriter(288)
	pkt.WriteByte(1) // 1 = level up; 2 = rank up
	pkt.WriteInt32(id)

	ctx.World.BroadcastSessionPacket(session, pkt)
	if currentClassRank > previousClassRank {
		sendClassRankUpEvent(session, id)
	}

	pkt = network.NewWriter(287)
	pkt.WriteByte(10) // 10 = level up
	for i := 0; i < 14; i++ {
		pkt.WriteByte(0)
	}
	pkt.WriteInt64(level)

	session.Send(pkt)
}

func UpdateHelpInfo(session *network.Session, reader *network.Reader) {
	_ = reader.ReadInt32() //helpIndex

	pkt := network.NewWriter(UPDATE_HELPINFO)
	pkt.WriteBool(true)

	session.Send(pkt)
}

func UpgradeSkill(session *network.Session, reader *network.Reader) {
	skillID := reader.ReadUint16()
	upgraded := false

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error("[UPGRADE_SKILL]", err)
	} else {
		ctx.Mutex.Lock()

		if ctx.Char == nil {
			log.Error("[UPGRADE_SKILL] Character is not initialized")
		} else {
			var (
				skill      skills.Skill
				slot       uint16
				matchCount int
			)

			for storedSlot, learnedSkill := range ctx.Char.Skills.List {
				if learnedSkill.Id != skillID {
					continue
				}
				if storedSlot < 0 || storedSlot > int(^uint16(0)) {
					log.Errorf("[UPGRADE_SKILL] Skill %d has invalid slot %d for character %d", skillID, storedSlot, ctx.Char.Id)
					continue
				}

				skill = learnedSkill
				slot = uint16(storedSlot)
				matchCount++
			}

			if matchCount == 0 {
				log.Errorf("[UPGRADE_SKILL] Skill %d is not learned by character %d", skillID, ctx.Char.Id)
			} else if matchCount > 1 {
				log.Errorf("[UPGRADE_SKILL] Skill %d occurs in multiple slots for character %d", skillID, ctx.Char.Id)
			} else if skill.Level == ^byte(0) {
				log.Errorf("[UPGRADE_SKILL] Skill %d in slot %d is already at the highest representable level", skill.Id, slot)
			} else {
				previousLevel := skill.Level
				skill.Level++
				skill.Slot = slot

				req := skills.SkillRequest{
					Server:        byte(g_ServerSettings.ServerId),
					Id:            ctx.Char.Id,
					PreviousLevel: previousLevel,
					Skill:         skill,
				}
				res := skills.SkillResponse{}

				if err := g_RPCHandler.Call(rpc.SaveSkill, &req, &res); err != nil {
					log.Errorf("[UPGRADE_SKILL] Unable to save skill %d in slot %d: %s", skill.Id, slot, err)
				} else if !res.Result {
					log.Errorf("[UPGRADE_SKILL] Skill %d in slot %d was not updated in the database", skill.Id, slot)
				} else {
					ctx.Char.Skills.Set(slot, skill)
					upgraded = true
					log.Infof("[UPGRADE_SKILL] Character %d upgraded skill %d in slot %d from level %d to %d",
						ctx.Char.Id, skill.Id, slot, previousLevel, skill.Level)
				}
			}
		}

		ctx.Mutex.Unlock()
	}

	pkt := network.NewWriter(UPGRADE_SKILL)
	pkt.WriteBool(upgraded)
	session.Send(pkt)
}

func UntrainSkill(session *network.Session, reader *network.Reader) {
	skillID := reader.ReadUint16()
	slot := uint16(reader.ReadByte())
	untrained := false

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error("[UNTRAIN_SKILL]", err)
	} else {
		ctx.Mutex.Lock()

		if ctx.Char == nil {
			log.Error("[UNTRAIN_SKILL] Character is not initialized")
		} else {
			skill := ctx.Char.Skills.Get(slot)
			levelInfo, exists := clientdata.FindSkillLevel(skillID, skill.Level)

			switch {
			case skill.Id == 0:
				log.Errorf("[UNTRAIN_SKILL] Slot %d is empty for character %d", slot, ctx.Char.Id)
			case skill.Id != skillID:
				log.Errorf("[UNTRAIN_SKILL] Skill %d does not match slot %d for character %d",
					skillID, slot, ctx.Char.Id)
			case skill.Level == 0:
				log.Errorf("[UNTRAIN_SKILL] Skill %d in slot %d has an invalid level", skillID, slot)
			case !exists:
				log.Errorf("[UNTRAIN_SKILL] Missing client data for skill %d level %d", skillID, skill.Level)
			case skill.Level == 1 && levelInfo.TrainType == clientdata.SkillTrainByQuest:
				log.Errorf("[UNTRAIN_SKILL] Quest skill %d cannot be removed", skillID)
			default:
				previousLevel := skill.Level
				req := skills.UntrainRequest{
					Server:        byte(g_ServerSettings.ServerId),
					Character:     ctx.Char.Id,
					SkillID:       skillID,
					Slot:          slot,
					PreviousLevel: previousLevel,
					Price:         levelInfo.UntrainPrice,
				}
				res := skills.UntrainResponse{}

				if err := g_RPCHandler.Call(rpc.UntrainSkill, &req, &res); err != nil {
					log.Errorf("[UNTRAIN_SKILL] Unable to save skill %d in slot %d: %s", skillID, slot, err)
				} else if !res.Result {
					log.Errorf("[UNTRAIN_SKILL] Skill %d in slot %d was not changed", skillID, slot)
				} else {
					ctx.Char.Alz = res.Alz
					if res.Level == 0 {
						ctx.Char.Skills.Remove(slot)
						ctx.Char.Links.RemoveSkillLocal(skillID)
					} else {
						skill.Level = res.Level
						ctx.Char.Skills.Set(slot, skill)
					}
					untrained = true
					log.Infof("[UNTRAIN_SKILL] Character %d changed skill %d in slot %d from level %d to %d for %d Alz",
						ctx.Char.Id, skillID, slot, previousLevel, res.Level, levelInfo.UntrainPrice)
				}
			}
		}

		ctx.Mutex.Unlock()
	}

	pkt := network.NewWriter(UNTRAIN_SKILL)
	pkt.WriteBool(untrained)
	session.Send(pkt)
}
