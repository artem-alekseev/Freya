package packet

import (
	"strings"

	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/event"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/server"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/script"

	"github.com/ubis/Freya/share/log"
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
	if hpChanged || mpChanged {
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
	pkt.WriteInt32(0x0100001F)

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
	pkt.WriteInt16(0x00)
	pkt.WriteByte(0x00)   // blessing bead count
	pkt.WriteByte(0x00)   // active quest count
	pkt.WriteUint16(0x00) // period item count
	pkt.WriteBytes(make([]byte, 1023))

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

	ctx.Mutex.Lock()
	ctx.Char = &c
	ctx.Warehouse = res.Warehouse
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

// Uninitialze Packet
func Uninitialze(session *network.Session, reader *network.Reader) {
	_ = reader.ReadUint16() // index
	_ = reader.ReadByte()   // map id
	_ = reader.ReadByte()   // log out

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
	_ = reader.ReadUint16()
	msglen := reader.ReadUint16()
	_ = reader.ReadInt16()
	_ = reader.ReadByte() // client message type

	const messageHeaderSize = 10
	const messageFieldsSize = 7 // data length, message length, reserved, message type
	if msglen < 3 {
		log.Errorf("Invalid MessageEvnt length: %d", msglen)
		return
	}

	textLen := int(msglen) - 3
	available := int(reader.Size) - messageHeaderSize - messageFieldsSize
	if textLen > available {
		log.Errorf("Invalid MessageEvnt payload: text=%d available=%d packet=%d", textLen, available, reader.Size)
		return
	}

	msg := reader.ReadString(textLen)

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

	pkt := SendMessage(session, msg)
	if pkt != nil {
		world.BroadcastSessionPacket(session, pkt)
	}
}

// WarpCommand packet
func WarpCommand(session *network.Session, reader *network.Reader) {
	warpId := reader.ReadByte()

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

	warp := world.FindWarp(warpId)
	if warp == nil {
		log.Error("Unable to find warp:", warpId)
		return
	}

	newWorld := wm.FindWorld(warp.World)
	if newWorld == nil {
		log.Error("Unable to find new world:", warp.World)
		return
	}

	pkt := network.NewWriter(WARPCOMMAND)
	pkt.WriteInt16(warp.Location[0].X) // pos x
	pkt.WriteInt16(warp.Location[0].Y) // pos y
	pkt.WriteInt32(0)                  // exp
	pkt.WriteInt32(0)                  // axp
	pkt.WriteInt32(0)                  // alz
	pkt.WriteInt32(0)                  // unk
	pkt.WriteInt16(session.UserIdx)
	pkt.WriteInt16(0x0100)
	pkt.WriteInt32(0x08)
	pkt.WriteByte(0)
	pkt.WriteInt32(warp.World)
	pkt.WriteInt32(0)
	pkt.WriteInt32(0)

	world.ExitWorld(session, server.DelUserWarp)

	session.Send(pkt)

	ctx.Mutex.Lock()
	ctx.Char.World = byte(warp.World)
	ctx.Char.X = byte(warp.Location[0].X)
	ctx.Char.Y = byte(warp.Location[0].Y)
	ctx.Char.BeginX = int16(warp.Location[0].X)
	ctx.Char.BeginY = int16(warp.Location[0].Y)
	ctx.Char.EndX = int16(warp.Location[0].X)
	ctx.Char.EndY = int16(warp.Location[0].Y)
	ctx.Mutex.Unlock()

	newWorld.EnterWorld(session)
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
	pkt.WriteUint32(session.UserIdx)
	pkt.WriteUint32(c.Level)
	pkt.WriteInt32(0x01C2)    // might be dwMoveBgnTime
	pkt.WriteUint16(c.BeginX) // start
	pkt.WriteUint16(c.BeginY)
	pkt.WriteUint16(c.EndX) // end
	pkt.WriteUint16(c.EndY)
	pkt.WriteByte(5) // pk - LVL
	pkt.WriteInt16(0)
	pkt.WriteInt16(0)
	pkt.WriteInt16(0)
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
	return SystemMessgEx(msg)
}
