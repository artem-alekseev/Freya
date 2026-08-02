package packet

import (
	"strings"

	"github.com/ubis/Freya/cmd/gameserver/clientdata"
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/event"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/server"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/script"

	"github.com/ubis/Freya/share/log"
)

const userObjectType uint32 = 0x01000000

func userObjectIndex(session *network.Session) uint32 {
	return userObjectType | uint32(session.UserIdx)
}

const (
	warpNPCDeadPK byte = 54 // NPCSIDX_DEAD_PK: death by another player
	warpNPCDead   byte = 63 // NPCSIDX_DEAD: normal death
	warpNPCGM     byte = 52 // NPCSIDX_GM: Ctrl+right-click GM warp
)

// Initialized Packet
func Initialized(session *network.Session, reader *network.Reader) {
	charId := reader.ReadInt32()

	if !session.Data.Verified || !session.Data.LoggedIn || session.DataEx == nil {
		log.Errorf("User is not verified (char: %d)", charId)
		return
	}

	ctx, err := context.PreParse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	// verify char id
	if (charId >> 3) != session.Data.AccountId {
		log.Errorf("User is using invalid character id (id: %d, char: %d)",
			session.Data.AccountId, charId)
		return
	}

	c := character.Character{}

	if len(session.Data.CharacterList) == 0 {
		// fetch characters
		reqList := character.ListReq{
			Account: session.Data.AccountId,
			Server:  byte(g_ServerSettings.ServerId),
		}
		resList := character.ListRes{}
		g_RPCHandler.Call(rpc.LoadCharacters, &reqList, &resList)

		session.Data.CharacterList = resList.List
	}

	// fetch character
	for _, data := range session.Data.CharacterList {
		if data.Id == charId {
			c = data
			break
		}
	}

	// check if character exists
	if c.Id != charId {
		log.Errorf("User is using invalid character id (id: %d, char: %d)",
			session.Data.AccountId, charId)
		return
	}

	hpChanged := c.RecalculateHP()
	mpChanged := c.RecalculateMP()
	spChanged := c.RecalculateSP()
	if hpChanged || mpChanged || spChanged {
		saveCharacterVitals(&c)
	}
	// load additional character data
	req := character.DataReq{
		Server: byte(g_ServerSettings.ServerId),
		Id:     c.Id,
	}
	res := character.DataRes{}
	g_RPCHandler.Call(rpc.LoadCharacterData, req, &res)
	if err := ensureBattleModeSkillsForCharacter(c.Id, c.Level, c.Style.BattleStyle, &res.Skills); err != nil {
		log.Errorf("Unable to grant battle mode skills for character %d: %s", c.Id, err)
	}

	// serialize data
	eq, eqlen := c.Equipment.Serialize()
	inv, invlen := res.Inventory.Serialize()
	sk, sklen := res.Skills.Serialize()
	sl, sllen := res.Links.Serialize()
	activeQuests := makeActiveQuestSlots(res.Quests)
	activeQuestCount := countActiveQuestSlots(activeQuests)

	pkt := network.NewWriter(INITIALIZED)
	pkt.WriteBytes(make([]byte, 57))
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x14)
	pkt.WriteByte(g_ServerSettings.ChannelId)
	pkt.WriteBytes(make([]byte, 23))
	pkt.WriteByte(0xFF)
	pkt.WriteUint16(g_ServerConfig.MaxUsers)
	pkt.WriteUint32(0x8501A8C0)
	pkt.WriteUint16(0x985A)
	pkt.WriteInt32(0x01)
	pkt.WriteUint32(userObjectIndex(session))

	pkt.WriteUint16(c.X)   // +
	pkt.WriteUint16(c.Y)   // +
	pkt.WriteUint64(c.Exp) // +
	pkt.WriteUint64(c.Alz)
	pkt.WriteUint64(c.WarExp) // +
	pkt.WriteUint32(c.Level)  // +

	pkt.WriteUint32(0)

	pkt.WriteUint32(c.STR) // +
	pkt.WriteUint32(c.DEX) // +
	pkt.WriteUint32(c.INT) // +
	pkt.WriteUint32(c.PNT) // +
	pkt.WriteByte(c.SwordRank)
	pkt.WriteByte(c.MagicRank)
	pkt.WriteUint16(0x00) // padding for skillrank
	pkt.WriteUint32(0x00)
	pkt.WriteUint16(c.MaxHP)     // +
	pkt.WriteUint16(c.CurrentHP) // +
	pkt.WriteUint16(c.MaxMP)     // +
	pkt.WriteUint16(c.CurrentMP) // +
	pkt.WriteUint16(c.MaxSP)     // +
	pkt.WriteUint16(c.CurrentSP) // +
	pkt.WriteUint16(0x00)        //stats.DungeonPoints)
	pkt.WriteUint16(0x00)
	pkt.WriteUint16(c.SwordExp)     // stats.SwordExp
	pkt.WriteUint16(c.SwordPoint)   // stats.SwordPoint
	pkt.WriteUint16(c.MagicExp)     // stats.MagicExp
	pkt.WriteUint16(c.MagicPoint)   // stats.MagicPoint
	pkt.WriteUint16(c.SwordRankExp) // stats.SwordExpPoint
	pkt.WriteUint16(c.MagicRankExp) // stats.MagicExpPoint
	pkt.WriteUint16(4)
	pkt.WriteUint16(4)
	pkt.WriteUint16(4)
	pkt.WriteUint16(4)
	//pkt.WriteInt32(0x2A30)
	pkt.WriteInt32(13107) // honour pnt +
	pkt.WriteUint16(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0x00)
	pkt.WriteByte(1)
	pkt.WriteByte(1)
	pkt.WriteByte(1)
	pkt.WriteByte(1)
	pkt.WriteByte(c.Nation) // Nation
	pkt.WriteInt32(0x00)
	pkt.WriteInt32(0xFFFF) // warp code
	pkt.WriteInt32(0xFFFF) // map code

	//pkt.WriteUint32(0x8501A8C0) // chat ip
	//pkt.WriteUint16(0x9858)     // chat port

	//pkt.WriteUint32(0x8501A8C0) // ah ip
	//pkt.WriteUint16(0x9859)     // ah port

	//pkt.WriteByte(0x01) // nation
	//pkt.WriteInt32(0x07) // warp code
	//pkt.WriteInt32(0x07) // map code
	pkt.WriteUint32(c.Style.Get())
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteUint32(0)
	pkt.WriteUint32(0) // Недостаточно места в инвентаре item
	pkt.WriteUint32(0)
	pkt.WriteUint32(0)
	pkt.WriteBytes(make([]byte, 24))
	pkt.WriteInt16(eqlen)  // +
	pkt.WriteInt16(invlen) // +
	pkt.WriteInt16(sklen)  // +
	pkt.WriteInt16(sllen)  // +
	pkt.WriteInt16(0x00)   // assistant count (low 2 bytes)
	pkt.WriteByte(0x00)    // assistant count (third byte)
	pkt.WriteByte(0x00)    // assistant count (high byte)
	pkt.WriteUint16(0x00)  // soul ability point total

	// The client reads bQuestNum at packet offset 0x147. The fixed tail
	// starts at offset 0x13E, immediately after soulAbilityPointTotal.
	// Keep all intermediate fields zero and place only the quest count here.
	userDataTail := make([]byte, 1023)
	userDataTail[0x147-0x13E] = activeQuestCount
	pkt.WriteBytes(userDataTail)

	pkt.WriteBytes(make([]byte, 128)) // quest dungeon flags
	pkt.WriteBytes(make([]byte, 128)) // mission dungeon flags

	pkt.WriteByte(0x00)   // Craft Lv 0
	pkt.WriteByte(0x00)   // Craft Lv 1
	pkt.WriteByte(0x00)   // Craft Lv 2
	pkt.WriteByte(0x00)   // Craft Lv 3
	pkt.WriteByte(0x00)   // Craft Lv 4
	pkt.WriteUint16(0x00) // Craft Exp 0
	pkt.WriteUint16(0x00) // Craft Exp 1
	pkt.WriteUint16(0x00) // Craft Exp 2
	pkt.WriteUint16(0x00) // Craft Exp 3
	pkt.WriteUint16(0x00) // Craft Exp 4
	pkt.WriteBytes(make([]byte, 16))
	pkt.WriteUint32(0x00) // Craft Type
	pkt.WriteUint32(0x00)
	pkt.WriteUint32(0x00)
	pkt.WriteUint32(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteByte(0x00)
	pkt.WriteByte(len(c.Name) + 1) //+
	pkt.WriteString(c.Name)        //+
	pkt.WriteBytes(eq)             //+
	pkt.WriteBytes(inv)            //+
	pkt.WriteBytes(sk)             //+
	pkt.WriteBytes(sl)             //+
	writeActiveQuestData(pkt, activeQuests)
	pkt.WriteBytes(make([]byte, 163))

	session.Send(pkt)

	// player is not moving anywhere, initialize begin/end movement variables
	c.BeginX = int16(c.X)
	c.BeginY = int16(c.Y)
	c.EndX = int16(c.X)
	c.EndY = int16(c.Y)

	// set-up inventory and links
	c.Inventory = &res.Inventory
	c.Links = &res.Links
	c.Skills = res.Skills

	// set-up RPC and data inside inventory to sync with the database
	c.Inventory.Setup(g_RPCHandler, c.Id, byte(g_ServerSettings.ServerId))

	// set-up RPC and data inside equipment to sync with the database
	c.Equipment.Setup(g_RPCHandler, c.Id, byte(g_ServerSettings.ServerId))

	// set-up RPC and data inside links to sync with the database
	c.Links.Setup(g_RPCHandler, c.Id, byte(g_ServerSettings.ServerId))

	ctx.CashInventory.Init(res.CashInventory)

	ctx.Mutex.Lock()
	ctx.Char = &c
	ctx.Warehouse = res.Warehouse
	ctx.ActiveQuests = activeQuests
	worldManager := ctx.WorldManager
	ctx.Mutex.Unlock()

	if worldManager == nil {
		log.Error("Unable to get world manager!")
		return
	}

	world := worldManager.FindWorld(c.World)
	if world == nil {
		log.Error("Unable to get world")
		return
	}

	world.EnterWorld(session)
	event.Trigger(event.PlayerJoin, session)
}

func makeActiveQuestSlots(quests []character.ActiveQuest) [context.QuestSlotCount]context.ActiveQuest {
	var slots [context.QuestSlotCount]context.ActiveQuest
	for _, active := range quests {
		if active.QuestID == 0 || active.Slot >= context.QuestSlotCount {
			continue
		}
		if slots[active.Slot].QuestID != 0 {
			continue
		}
		slots[active.Slot] = active
	}
	return slots
}

func countActiveQuestSlots(quests [context.QuestSlotCount]context.ActiveQuest) byte {
	var count byte
	for _, quest := range quests {
		if quest.QuestID != 0 {
			count++
		}
	}
	return count
}

func writeActiveQuestData(pkt *network.Writer, quests [context.QuestSlotCount]context.ActiveQuest) {
	for slot, quest := range quests {
		if quest.QuestID == 0 {
			continue
		}

		// QUESTLIST_DATA0 in S2C_INITIALIZED is packed as:
		// quest id, NPC flag, UI state, slot, then mission counters.
		pkt.WriteUint16(quest.QuestID)
		pkt.WriteUint16(quest.NPCFlags)
		pkt.WriteByte(quest.ShowDesc)
		pkt.WriteByte(quest.Expand)
		pkt.WriteByte(byte(slot))

		for counter := byte(0); counter < clientdata.QuestMissionCount(quest.QuestID); counter++ {
			pkt.WriteByte(0)
		}
	}
}

// Uninitialze Packet
func Uninitialze(session *network.Session, reader *network.Reader) {
	_ = reader.ReadUint16() // index
	_ = reader.ReadByte()   // map id
	_ = reader.ReadByte()   // log out

	if ctx, err := context.Parse(session); err == nil {
		resetBattleMode(session, ctx)
	}

	SaveCharacterPosition(session)
	// The character list is reused while the TCP session remains connected.
	// Force the next Initialized packet to load the just-saved coordinates.
	session.Data.CharacterList = nil

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := network.NewWriter(UNINITIALZE)
	pkt.WriteByte(0) // result

	// complete - 0x00
	// fail - 0x01
	// ignored - 0x02
	// busy - 0x03
	// anti online game - 0x30

	session.Send(pkt)

	world.ExitWorld(session, server.DelUserLogout)
}

// MessageEvnt Packet
func MessageEvnt(session *network.Session, reader *network.Reader) {
	const messageHeaderSize = 10
	const dataLengthSize = 2
	if int(reader.Size) < messageHeaderSize+dataLengthSize {
		log.Errorf("Invalid MessageEvnt packet size: %d", reader.Size)
		return
	}

	dataLen := reader.ReadUint16()
	available := int(reader.Size) - messageHeaderSize - dataLengthSize
	if int(dataLen) > available {
		log.Errorf("Invalid MessageEvnt payload: data=%d available=%d packet=%d", dataLen, available, reader.Size)
		return
	}

	data := reader.ReadBytes(int(dataLen))
	log.Debugf("MessageEvnt raw payload: dataLen=%d bytes=% X", dataLen, data)

	if len(data) < 5 {
		log.Errorf("Invalid MessageEvnt message data: length=%d", len(data))
		return
	}

	msglen := uint16(data[0]) | uint16(data[1])<<8
	if msglen < 3 {
		log.Errorf("Invalid MessageEvnt length: %d", msglen)
		return
	}

	textLen := int(msglen) - 3
	if textLen > len(data)-5 {
		log.Errorf("Invalid MessageEvnt text length: text=%d available=%d", textLen, len(data)-5)
		return
	}

	msg := string(data[5 : 5+textLen])

	if strings.HasPrefix(msg, "#") {
		parts := strings.Split(msg, " ")
		command := parts[0][1:] // remove the '#'
		args := parts[1:]

		if err := script.ExecCommand(command, args, session); err != nil {
			log.Error("Failed to execute Lua command:", err)
		}

		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	pkt := notifyMessage(session, data)
	if pkt != nil {
		world.BroadcastSessionPacket(session, pkt)
	}
}

// WarpCommand packet
func WarpCommand(session *network.Session, reader *network.Reader) {
	const warpCommandPacketSize = 29 // C2S_WARPCOMMAND, including GMWarpData

	if reader.Size < 17 {
		log.Warningf("WarpCommand: invalid packet size %d", reader.Size)
		return
	}

	npcIndex := reader.ReadByte()
	slotIndex := reader.ReadUint16()
	warpArgument := reader.ReadUint32()
	log.Debugf("WarpCommand: npc=%d slot=%d argument=%d", npcIndex, slotIndex, warpArgument)

	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	wm := context.GetWorldManager(session)
	if wm == nil {
		log.Error("Unable to get world manager!")
		return
	}

	world := context.GetWorld(session)
	if world == nil {
		log.Error("Unable to get current world!")
		return
	}

	var warpPoint clientdata.WarpPoint
	var found bool
	switch npcIndex {
	case warpNPCGM:
		if reader.Size != warpCommandPacketSize {
			log.Warningf("WarpCommand: invalid GM warp packet size %d", reader.Size)
			return
		}

		// WorldSvr treats the union as WORD position and decodes it as
		// HIBYTE=X, LOBYTE=Y. The remaining fields describe the GM warp
		// context; NPCSIDX_GM itself always stays in the current world.
		gmWorld := reader.ReadInt32()
		gmDungeon := reader.ReadInt32()
		gmTarget := reader.ReadInt32()
		position := uint16(warpArgument)
		warpPoint = clientdata.WarpPoint{
			World: world.GetId(),
			Locations: [3]clientdata.WarpLocation{
				{X: byte(position >> 8), Y: byte(position)},
				{X: byte(position >> 8), Y: byte(position)},
				{X: byte(position >> 8), Y: byte(position)},
			},
		}
		found = true
		log.Debugf("GM warp: world=%d dungeon=%d target=%d x=%d y=%d",
			gmWorld, gmDungeon, gmTarget, warpPoint.Locations[0].X, warpPoint.Locations[0].Y)
	case 61: // NPCSIDX_NAVI: GPS uses the server warp index directly.
		points := clientdata.WarpPoints()
		if int(warpArgument) < len(points) {
			warpPoint = points[warpArgument]
			found = true
		}
	case 62: // NPCSIDX_RETN: return to the nation-specific city point.
		warpPoint, found = clientdata.FindReturnPoint(world.GetId(), ctx.Char.Nation)
	case warpNPCDead, warpNPCDeadPK:
		// The client uses the dead NPC index and sends zero in the union field.
		// The destination is the current map's dead_warp from cabal.dec, not an
		// entry in warp_npc.
		warpPoint, found = clientdata.FindStartingPoint(world.GetId(), ctx.Char.Nation)
	case 56: // NPCSIDS_BPNT: starting point or a map transition.
		// This command carries 0xffff in its union. The client route is
		// selected by the slot/order index from cabal.dec's warp_npc table.
		warpPoint, found = clientdata.FindMapTransition(world.GetId(), slotIndex)
		if !found {
			warpPoint, found = clientdata.FindStartingPoint(world.GetId(), ctx.Char.Nation)
		}
	default:
		// Normal NPC transitions are resolved directly from cabal.dec's
		// source-world/NPC/order table.
		warpPoint, found = clientdata.FindNPCWarp(world.GetId(), npcIndex, warpArgument)
	}
	if !found {
		log.Errorf("Unable to resolve warp: npc=%d argument=%d world=%d", npcIndex, warpArgument, world.GetId())
		return
	}

	warp := context.Warp{
		Id:    warpPoint.ID,
		World: warpPoint.World,
		Code:  warpPoint.Code,
		Fee:   warpPoint.Fee,
		Level: warpPoint.Level,
	}
	for i, location := range warpPoint.Locations {
		warp.Location[i] = context.WarpLocation{X: location.X, Y: location.Y}
	}

	location := warp.Location[0]
	if ctx.Char.Nation >= 1 && ctx.Char.Nation <= 2 {
		location = warp.Location[ctx.Char.Nation]
	}

	newWorld := wm.FindWorld(warp.World)
	if newWorld == nil {
		log.Error("Unable to find new world:", warp.World)
		return
	}

	revived := false
	var revivedHP, revivedMP uint16
	if npcIndex == warpNPCDead || npcIndex == warpNPCDeadPK {
		ctx.Mutex.Lock()
		if ctx.Char.CurrentHP != 0 {
			currentHP := ctx.Char.CurrentHP
			ctx.Mutex.Unlock()
			log.Warningf("Rejected death warp for living character %d: hp=%d", ctx.Char.Id, currentHP)
			return
		}

		// WorldSvr's RecycleUserByDead restores both resources completely for
		// the normal/dead-PK warp. Battle mode is also stopped on death.
		ctx.Char.CurrentHP = ctx.Char.MaxHP
		ctx.Char.CurrentMP = ctx.Char.MaxMP
		revivedHP = ctx.Char.CurrentHP
		revivedMP = ctx.Char.CurrentMP
		ctx.Mutex.Unlock()

		resetBattleMode(session, ctx)
		if !saveContextVitals(ctx) {
			log.Warningf("Unable to save revived vitals for character %d", ctx.Char.Id)
		}
		revived = true
	}

	if npcIndex == 62 {
		item := ctx.Char.Inventory.Get(slotIndex)
		if !clientdata.IsReturnStone(item.Kind) || item.Option <= 0 {
			log.Warningf("Rejected return stone use: character %d, item %d, slot %d", ctx.Char.Id, item.Kind, slotIndex)
			return
		}

		updatedItem, consumed, err := ctx.Char.Inventory.ConsumeOne(slotIndex)
		if err != nil {
			log.Errorf("Unable to consume return stone: character %d, slot %d: %s", ctx.Char.Id, slotIndex, err.Error())
			return
		}
		if !consumed {
			log.Warningf("Return stone was not consumed: character %d, slot %d", ctx.Char.Id, slotIndex)
			return
		}
		log.Debugf("Consumed return stone: character %d, slot %d, remaining %d", ctx.Char.Id, slotIndex, updatedItem.Option)
	}

	ctx.Mutex.RLock()
	exp := ctx.Char.Exp
	alz := ctx.Char.Alz
	ctx.Mutex.RUnlock()

	ctx.Mutex.Lock()
	ctx.Char.World = byte(warp.World)
	ctx.Char.X = byte(location.X)
	ctx.Char.Y = byte(location.Y)
	ctx.Char.BeginX = int16(location.X)
	ctx.Char.BeginY = int16(location.Y)
	ctx.Char.EndX = int16(location.X)
	ctx.Char.EndY = int16(location.Y)
	ctx.Mutex.Unlock()

	pkt := network.NewWriter(WARPCOMMAND)
	pkt.WriteInt16(location.X)                // pos x
	pkt.WriteInt16(location.Y)                // pos y
	pkt.WriteInt64(exp)                       // exp (TEXP)
	pkt.WriteInt64(alz)                       // alz (TALZ)
	pkt.WriteUint32(userObjectIndex(session)) // user object index
	pkt.WriteInt32(0x08)                      // WorldRegion::Neutral
	pkt.WriteByte(0)                          // SERVER_RESULT::COMPLETE
	pkt.WriteInt32(warp.World)
	pkt.WriteInt32(0)
	pkt.WriteInt32(0)

	if warp.World == world.GetId() {
		if revived {
			// A dead character is already rendered as a corpse by nearby clients.
			// Remove that entry from the old cell before announcing the revived
			// character in the new one, otherwise the corpse remains visible.
			world.ExitWorld(session, server.DelUserWarp)
			newWorld.EnterWorldWithReason(session, server.NewUserWarp)
		} else {
			// WorldSvr handles a same-world warp by moving the character and
			// relinking its cell, not by removing and re-adding the world object.
			world.AdjustCell(session)
			// Acknowledge the warp before sending the snapshot. The client keeps
			// resending WarpCommand until this packet has been received.
			session.Send(pkt)
			world.RefreshPlayer(session)
			world.BroadcastSessionPacket(session, NewUserSingle(session, server.NewUserWarp))
		}
		if revived {
			// Send the warp acknowledgement after the old corpse has been
			// removed and the live character has been linked again.
			session.Send(pkt)
		}
	} else {
		world.ExitWorld(session, server.DelUserWarp)
		newWorld.EnterWorld(session)
		session.Send(pkt)
	}

	if revived {
		sendHealthUpdate(session, revivedHP)
		sendManaUpdate(session, revivedMP)
		log.Infof("Character %d revived at dead warp: world=%d x=%d y=%d hp=%d mp=%d",
			ctx.Char.Id, warp.World, location.X, location.Y, revivedHP, revivedMP)
	}

	SaveCharacterPosition(session)
}

func fillPlayerInfo(pkt *network.Writer, session *network.Session) {
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return
	}

	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	c := ctx.Char

	if c == nil {
		// client is not ready
		return
	}
	eq, eqlen := c.Equipment.SerializeEx()
	pkt.WriteUint32(c.Id)
	pkt.WriteUint32(userObjectIndex(session))
	pkt.WriteUint32(c.Level)
	pkt.WriteInt32(0x01C2)    // might be dwMoveBgnTime
	pkt.WriteUint16(c.BeginX) // start
	pkt.WriteUint16(c.BeginY)
	pkt.WriteUint16(c.EndX) // end
	pkt.WriteUint16(c.EndY)
	pkt.WriteUint16(0) // PK level (normal character)
	pkt.WriteByte(c.Nation)
	pkt.WriteInt32(0) // reserved
	pkt.WriteInt32(c.Style.Get())
	pkt.WriteByte(c.LiveStyle) // animation id aka "live style"
	pkt.WriteInt16(0)
	pkt.WriteInt16(eqlen)
	pkt.WriteByte(0) // trade
	pkt.WriteByte(0)
	pkt.WriteInt32(0x01) // title
	pkt.WriteByte(0x0)
	pkt.WriteByte(0x0)
	pkt.WriteByte(0x0)
	pkt.WriteByte(0x0)
	pkt.WriteInt16(0)
	pkt.WriteByte(c.Nation)
	pkt.WriteInt16(0)
	pkt.WriteInt16(0)
	pkt.WriteByte(len(c.Name) + 1)
	pkt.WriteString(c.Name)
	pkt.WriteByte(1) // guild name len
	pkt.WriteString("1")
	pkt.WriteBytes(eq)
}

func NewUserSingle(session *network.Session, reason server.NewUserType) *network.Writer {
	pkt := network.NewWriter(NEWUSERLIST)
	pkt.WriteByte(1) // player num
	pkt.WriteByte(byte(reason))

	fillPlayerInfo(pkt, session)

	return pkt
}

func NewUserList(players map[uint16]*network.Session, reason server.NewUserType) *network.Writer {
	online := len(players)

	pkt := network.NewWriter(NEWUSERLIST)
	pkt.WriteByte(online)
	pkt.WriteByte(byte(reason))

	for _, v := range players {
		fillPlayerInfo(pkt, v)
	}

	return pkt
}

// DelUserList to all already connected players
func DelUserList(session *network.Session, reason server.DelUserType) *network.Writer {
	charId, err := context.GetCharId(session)
	if err != nil {
		log.Error(err.Error())
		return nil
	}

	pkt := network.NewWriter(DELUSERLIST)
	pkt.WriteUint32(charId)
	pkt.WriteByte(byte(reason)) // type

	/* types:
	 * dead = 0x10
	 * warp = 0x11
	 * logout = 0x12
	 * retn = 0x13
	 * dissapear = 0x14
	 * nfsdead = 0x15
	 */

	return pkt
}

func SendMessage(session *network.Session, msg string) *network.Writer {
	characterID, err := messageSender(session)
	if err != nil {
		log.Error("Unable to get character for chat message:", err)
		return nil
	}

	data := buildNormalMessagePayload(msg)
	return newMessageNotification(characterID, data)
}

func notifyMessage(session *network.Session, data []byte) *network.Writer {
	characterID, err := messageSender(session)
	if err != nil {
		log.Error("Unable to get character for chat notification:", err)
		return nil
	}

	return newMessageNotification(characterID, data)
}

func messageSender(session *network.Session) (int32, error) {
	ctx, err := context.Parse(session)
	if err != nil {
		return 0, err
	}

	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()

	return ctx.Char.Id, nil
}

func newMessageNotification(characterID int32, data []byte) *network.Writer {
	if len(data) > 0xFFFF {
		log.Errorf("Chat message payload is too large: %d", len(data))
		return nil
	}

	packet := network.NewWriter(NFY_MESSAGEEVNT)
	packet.WriteInt32(characterID)
	packet.WriteUint16(uint16(len(data)))
	packet.WriteBytes(data)
	return packet
}

func buildNormalMessagePayload(msg string) []byte {
	data := make([]byte, len(msg)+7)
	messageLength := len(msg) + 3
	data[0] = byte(messageLength)
	data[1] = byte(messageLength >> 8)
	data[2] = 0xFE
	data[3] = 0xFE
	data[4] = 0xA0
	copy(data[5:], msg)
	// The client expects two trailing bytes after the message text.
	data[len(data)-2] = 0
	data[len(data)-1] = 0
	return data
}
