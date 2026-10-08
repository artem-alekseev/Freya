package chat

import (
	"sync"
	"time"

	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/buddy"
	"github.com/ubis/Freya/share/models/guild"
	"github.com/ubis/Freya/share/models/mail"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/textcodec"
)

const (
	chatNodeCommandBase         = 400
	connect2Svr                 = chatNodeCommandBase + 1
	chatAuthAccount             = chatNodeCommandBase + 2
	chatLogin                   = chatNodeCommandBase + 3
	chatLogout                  = chatNodeCommandBase + 4
	charGameStateChange         = chatNodeCommandBase + 10
	guildCreate                 = chatNodeCommandBase + 32
	chatNotifyUserConnect       = chatNodeCommandBase + 7
	heartbeat                   = chatNodeCommandBase + 12
	buddyGetGroupList           = chatNodeCommandBase + 61
	buddyGetMemberList          = chatNodeCommandBase + 62
	buddyGetBlacklist           = chatNodeCommandBase + 63
	buddyMemberMemoModify       = chatNodeCommandBase + 70
	buddyMemberDelete           = chatNodeCommandBase + 71
	buddyMemberDeleteNotify     = chatNodeCommandBase + 72
	buddyMemberAdd              = chatNodeCommandBase + 74
	buddyMemberRequest          = chatNodeCommandBase + 75
	buddyMemberResponse         = chatNodeCommandBase + 78
	equipmentViewRequest        = 2001
	equipmentViewNotifyRequest  = 2002
	equipmentViewResponse       = 2003
	equipmentViewNotifyResponse = 2004
	mailReceivedList            = 2005
	mailSentList                = 2006
	mailUpdateList              = 2007
	mailNewReceived             = 2008
)

const (
	equipmentViewResultOK     int32 = 0
	equipmentViewResultLogout int32 = 1
	equipmentViewResultAvoid  int32 = 2
	equipmentViewDataSize           = 18 // EQUIPMENT_DATA2 in server8files/Share/Protosdef.h
	equipmentViewMaxCount           = 21 // EQUIPMENT_SLOTNUM with USE_PET_EXPANSION
)

type State struct {
	CharacterID     int32
	Server          byte
	ChatServerID    uint32
	PresenceStatus  byte
	ChannelID       byte
	WorldID         byte
	DungeonID       uint16
	Level           byte
	Nation          byte
	GuildNo         uint32
	EntryLimitBase  byte
	EntryLimitRange byte
	Authorized      bool
	LoggedIn        bool
}

const (
	chatNodeServerType  = 0xFA
	chatNodeGroupIndex  = 1
	presenceOnline      = 1
	presenceOffline     = 2
	presenceStatusEnd   = 7
	stateChangePresence = 0x0001
)

var g_RPCHandler *rpc.Client
var g_NetworkManager *network.Network

type pendingBuddyInvite struct {
	Requester buddy.Character
	Target    buddy.Character
}

var pendingBuddyInvites = struct {
	sync.Mutex
	byTarget map[int32]pendingBuddyInvite
}{byTarget: make(map[int32]pendingBuddyInvite)}

var pendingEquipmentViews = struct {
	sync.Mutex
	byTarget map[int32]map[int32]struct{}
}{byTarget: make(map[int32]map[int32]struct{})}

func SetRPCHandler(handler *rpc.Client) {
	g_RPCHandler = handler
}

func SetNetworkManager(manager *network.Network) {
	g_NetworkManager = manager
}

// HandlePacket handles the ChatNode handshake and the initial mail-list
// exchange. ChatNode has its own process and therefore its own packet
// namespace, separate from the GameServer packet handler.
func HandlePacket(session *network.Session, reader *network.Reader) {
	state, ok := session.DataEx.(*State)
	if !ok {
		state = &State{}
		session.DataEx = state
	}

	switch reader.Type {
	case connect2Svr:
		handleConnect(session)
	case chatAuthAccount:
		handleAuth(session, reader, state)
	case chatLogin:
		handleLogin(session, reader, state)
	case chatLogout:
		handleLogout(session, state)
	case charGameStateChange:
		handleCharGameStateChange(session, state, reader)
	case guildCreate:
		handleGuildCreate(session, state, reader)
	case heartbeat:
		// Client keepalive; ChatNode does not send a response.
	case buddyGetGroupList:
		handleBuddyGroupList(session, state, reader)
	case buddyGetMemberList:
		handleBuddyMemberList(session, state, reader)
	case buddyGetBlacklist:
		sendBuddyBlacklist(session)
	case buddyMemberMemoModify:
		handleBuddyMemberMemoModify(session, state, reader)
	case buddyMemberDelete:
		handleBuddyMemberDelete(session, state, reader)
	case buddyMemberAdd:
		handleBuddyMemberAdd(session, state, reader)
	case buddyMemberResponse:
		handleBuddyMemberResponse(session, state, reader)
	case equipmentViewRequest:
		handleEquipmentViewRequest(session, state, reader)
	case equipmentViewResponse:
		handleEquipmentViewResponse(session, state, reader)
	case buddyMemberRequest:
		// Server-to-client request opcode. The client does not send it back.
	case mailReceivedList:
		sendMailList(session, state, mailReceivedList)
	case mailSentList:
		sendMailList(session, state, mailSentList)
	case mailUpdateList:
		lastMailID := int32(0)
		if reader.Size >= 14 {
			lastMailID = reader.ReadInt32()
		}
		sendMailUpdate(session, state, lastMailID)
	default:
		log.Debugf("Ignoring unsupported ChatNode packet: type=%d", reader.Type)
	}
}

// ClearBuddyInvites removes invitations involving a disconnected character.
// Without this cleanup, a stale invitation could block a new request until
// the ChatServer process is restarted.
func ClearBuddyInvites(characterID int32) {
	pendingBuddyInvites.Lock()
	defer pendingBuddyInvites.Unlock()
	for targetID, invite := range pendingBuddyInvites.byTarget {
		if invite.Requester.ID == characterID || invite.Target.ID == characterID {
			delete(pendingBuddyInvites.byTarget, targetID)
		}
	}
}

// ClearEquipmentViewRequests removes equipment-view requests owned by or
// addressed to a disconnected character.
func ClearEquipmentViewRequests(characterID int32) {
	if characterID <= 0 {
		return
	}
	pendingEquipmentViews.Lock()
	defer pendingEquipmentViews.Unlock()
	delete(pendingEquipmentViews.byTarget, characterID)
	for targetID, requesters := range pendingEquipmentViews.byTarget {
		delete(requesters, characterID)
		if len(requesters) == 0 {
			delete(pendingEquipmentViews.byTarget, targetID)
		}
	}
}

func rememberEquipmentViewRequest(targetID, requesterID int32) {
	pendingEquipmentViews.Lock()
	defer pendingEquipmentViews.Unlock()
	requesters := pendingEquipmentViews.byTarget[targetID]
	if requesters == nil {
		requesters = make(map[int32]struct{})
		pendingEquipmentViews.byTarget[targetID] = requesters
	}
	requesters[requesterID] = struct{}{}
}

func consumeEquipmentViewRequest(targetID, requesterID int32) bool {
	pendingEquipmentViews.Lock()
	defer pendingEquipmentViews.Unlock()
	requesters := pendingEquipmentViews.byTarget[targetID]
	if requesters == nil {
		return false
	}
	if _, ok := requesters[requesterID]; !ok {
		return false
	}
	delete(requesters, requesterID)
	if len(requesters) == 0 {
		delete(pendingEquipmentViews.byTarget, targetID)
	}
	return true
}

// NotifyBuddyPresence refreshes the member list of every online friend when a
// character enters or leaves ChatNode. The client learns online state from
// the presenceStatus field inside the 462 member-list packet and displays the
// 410-style login notification only for this explicit enter/leave transition.
func NotifyBuddyPresence(serverID byte, characterID int32) {
	notifyBuddyPresence(serverID, characterID, true)
}

// refreshBuddyPresence propagates a state change (for example, a channel or
// dungeon update) without sending the client-facing "joined channel" toast.
// WorldSvr sends those changes as member/state updates, not as a new login.
func refreshBuddyPresence(serverID byte, characterID int32) {
	notifyBuddyPresence(serverID, characterID, false)
}

func notifyBuddyPresence(serverID byte, characterID int32, announce bool) {
	if g_RPCHandler == nil || characterID <= 0 {
		return
	}
	response := buddy.ListResponse{}
	if err := g_RPCHandler.Call(rpc.LoadBuddyList, &buddy.ListRequest{
		Server: serverID, Character: characterID,
	}, &response); err != nil {
		log.Errorf("ChatNode buddy presence load failed: character=%d: %s", characterID, err)
		return
	}
	character := buddy.LookupResponse{}
	if err := g_RPCHandler.Call(rpc.LookupBuddyCharacter, &buddy.LookupRequest{
		Server: serverID, CharacterID: characterID,
	}, &character); err != nil || !character.Found {
		character.Character.ID = characterID
	}
	connected := buddySessionOnline(characterID)
	for _, member := range response.Members {
		friendSession, friendState := buddySession(member.ID)
		if friendSession != nil && friendState != nil {
			if announce {
				sendBuddyPresenceNotification(friendSession, serverID, character.Character, connected)
			}
			sendBuddyMemberList(friendSession, friendState, 0)
		}
	}
	log.Debugf("ChatNode buddy presence changed: character=%d online=%t friends=%d announce=%t",
		characterID, buddySessionOnline(characterID), len(response.Members), announce)
}

func sendBuddyPresenceNotification(session *network.Session, serverID byte,
	character buddy.Character, connected bool) {
	if session == nil || character.ID <= 0 {
		return
	}

	status, chatID := buddyPresence(character.ID)
	if !connected {
		status = presenceOffline
		chatID = 0
	}
	presenceState := buddyStateForCharacter(character.ID)
	pkt := network.NewWriter(chatNotifyUserConnect)
	pkt.WriteUint32(chatID)
	pkt.WriteInt32(character.ID)
	pkt.WriteUint32(character.GuildNo)
	pkt.WriteByte(serverID)
	if presenceState != nil {
		pkt.WriteByte(presenceState.ChannelID)
		pkt.WriteByte(presenceState.WorldID)
	} else {
		pkt.WriteByte(0)
		pkt.WriteByte(0)
	}
	pkt.WriteUint32(charRemark(character.BattleStyle, character.Nation, character.ClassLevel))
	pkt.WriteByte(character.Level)
	if presenceState != nil {
		pkt.WriteByte(presenceState.EntryLimitBase)
		pkt.WriteByte(presenceState.EntryLimitRange)
	} else {
		pkt.WriteByte(0)
		pkt.WriteByte(255)
	}
	pkt.WriteBool(connected)
	session.Send(pkt)
	log.Debugf("ChatNode buddy presence notification: target=%d connected=%t status=%d chatSvrId=%08X",
		character.ID, connected, status, chatID)
}

// NotifyMailReceived is called by MasterServer after a new mail is committed.
// ChatNode's client-facing notification has no payload; the client requests
// the actual mail data with packet 2007 after receiving it.
func NotifyMailReceived(_ *rpc.Client, request *mail.NewReceivedRequest, _ *mail.NewReceivedResponse) error {
	if request == nil || g_NetworkManager == nil || request.Character <= 0 {
		return nil
	}
	for _, session := range g_NetworkManager.Sessions() {
		state, ok := session.DataEx.(*State)
		if !ok || !state.LoggedIn || state.CharacterID != request.Character || session.IsClosed() {
			continue
		}
		session.Send(network.NewWriter(mailNewReceived))
		log.Debugf("ChatNode new-mail notification: character=%d", request.Character)
		break
	}
	return nil
}

func handleBuddyGroupList(session *network.Session, state *State, reader *network.Reader) {
	if reader.Size < 14 {
		log.Warningf("Invalid buddy group-list packet size: %d", reader.Size)
		return
	}
	reserved := reader.ReadUint32()
	pkt := network.NewWriter(buddyGetGroupList)
	pkt.WriteUint32(reserved)
	data := network.NewWriter(0)
	data.WriteUint32(buddy.DefaultGroup)
	data.WriteUint32(buddy.DefaultGroup)
	writeForwardString(data, "Друзья", 20)
	encoded := dataBytes(data)
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
	session.Send(pkt)
	log.Debugf("ChatNode buddy group list response: character=%d", state.CharacterID)
}

func handleBuddyMemberList(session *network.Session, state *State, reader *network.Reader) {
	if reader.Size < 14 {
		log.Warningf("Invalid buddy member-list packet size: %d", reader.Size)
		return
	}
	reserved := reader.ReadUint32()
	sendBuddyMemberList(session, state, reserved)
}

// ChatNode's blacklist is not stored by Freya yet. WorldSvr still expects a
// response to this request during character login, so return a valid empty
// list instead of leaving the client waiting for data.
func sendBuddyBlacklist(session *network.Session) {
	if session == nil {
		return
	}
	pkt := network.NewWriter(buddyGetBlacklist)
	pkt.WriteUint16(0) // dataLen
	session.Send(pkt)
}

func handleBuddyMemberDelete(session *network.Session, state *State, reader *network.Reader) {
	if state.CharacterID <= 0 || reader.Size < 14 {
		sendBuddyDeleteResult(session, buddy.DeleteInvalidParam, 0)
		return
	}

	targetID := int32(reader.ReadUint32())
	if targetID <= 0 || uint32(targetID) == ^uint32(0) || targetID == state.CharacterID {
		sendBuddyDeleteResult(session, buddy.DeleteInvalidParam, 0)
		return
	}

	response := buddy.DeleteResponse{Result: buddy.DeleteInvalidParam}
	if g_RPCHandler == nil || g_RPCHandler.Call(rpc.DeleteBuddy, &buddy.DeleteRequest{
		Server: configuredServer(state), Character: state.CharacterID, Target: targetID,
	}, &response) != nil {
		response.Result = buddy.DeleteDBFail
	}
	if response.Result != buddy.DeleteOK {
		sendBuddyDeleteResult(session, response.Result, 0)
		return
	}

	sendBuddyDeleteResult(session, response.Result, targetID)
	if targetSession, targetState := buddySession(targetID); targetSession != nil && targetState != nil {
		sendBuddyDeleteNotification(targetSession, state.CharacterID, targetID)
		sendBuddyMemberList(targetSession, targetState, 0)
	}
	sendBuddyMemberList(session, state, 0)
	log.Debugf("ChatNode buddy member deleted: character=%d target=%d",
		state.CharacterID, targetID)
}

func handleEquipmentViewRequest(session *network.Session, state *State, reader *network.Reader) {
	if state.CharacterID <= 0 || reader.Size < 14 {
		sendEquipmentViewResult(session, equipmentViewResultAvoid, 0, 0, nil)
		return
	}

	targetID := reader.ReadInt32()
	hasMustAllow := reader.Size >= 15
	mustAllow := false
	if hasMustAllow {
		mustAllow = reader.ReadByte() != 0
	}
	if targetID <= 0 {
		sendEquipmentViewResult(session, equipmentViewResultAvoid, targetID, 0, nil)
		return
	}

	targetSession, _ := buddySession(targetID)
	if targetSession == nil {
		sendEquipmentViewResult(session, equipmentViewResultLogout, targetID, 0, nil)
		return
	}

	rememberEquipmentViewRequest(targetID, state.CharacterID)
	pkt := network.NewWriter(equipmentViewNotifyRequest)
	pkt.WriteInt32(state.CharacterID)
	if hasMustAllow {
		pkt.WriteBool(mustAllow)
	}
	targetSession.Send(pkt)
	log.Debugf("ChatNode equipment view request: requester=%d target=%d mustAllow=%t",
		state.CharacterID, targetID, mustAllow)
}

func handleEquipmentViewResponse(_ *network.Session, state *State, reader *network.Reader) {
	if state.CharacterID <= 0 || reader.Size < 20 {
		log.Warningf("Invalid equipment-view response packet size: %d", reader.Size)
		return
	}

	result := reader.ReadInt32()
	requesterID := reader.ReadInt32()
	targetBattleStyle := reader.ReadByte()
	count := reader.ReadByte()
	dataLen := int(count) * equipmentViewDataSize
	if count > equipmentViewMaxCount || dataLen > int(reader.Size)-20 {
		log.Warningf("Invalid equipment-view response payload: character=%d count=%d packetSize=%d",
			state.CharacterID, count, reader.Size)
		return
	}
	data := reader.ReadBytes(dataLen)
	if !consumeEquipmentViewRequest(state.CharacterID, requesterID) {
		log.Warningf("Unmatched equipment-view response: target=%d requester=%d",
			state.CharacterID, requesterID)
		return
	}

	requesterSession, _ := buddySession(requesterID)
	if requesterSession == nil {
		return
	}
	sendEquipmentViewResult(requesterSession, result, state.CharacterID, targetBattleStyle, data)
	log.Debugf("ChatNode equipment view response: requester=%d target=%d result=%d count=%d",
		requesterID, state.CharacterID, result, count)
}

func handleCharGameStateChange(session *network.Session, state *State, reader *network.Reader) {
	// C2S_CHARGAMESTATECHANGE is a fixed 25-byte packet for this client:
	// header, character index, change mask, state fields and two entry-limit
	// bytes. The character index from the packet is not trusted; ChatNode binds
	// the update to the authenticated session, like WorldSvr does.
	if state.CharacterID <= 0 || reader.Size < 25 {
		log.Warningf("Invalid ChatNode character-state packet size: %d", reader.Size)
		return
	}

	_ = reader.ReadUint32()
	changeFlag := reader.ReadUint16()
	presenceStatus := reader.ReadByte()
	channelID := reader.ReadByte()
	worldID := reader.ReadByte()
	dungeonID := reader.ReadUint16()
	level := reader.ReadByte()
	nation := reader.ReadByte()
	entryLimitBase := reader.ReadByte()
	entryLimitRange := reader.ReadByte()
	if changeFlag == 0 {
		return
	}
	if changeFlag&stateChangePresence != 0 &&
		(presenceStatus == 0 || presenceStatus >= presenceStatusEnd) {
		log.Warningf("Invalid ChatNode presence status: character=%d status=%d",
			state.CharacterID, presenceStatus)
		return
	}

	if changeFlag&stateChangePresence != 0 {
		state.PresenceStatus = presenceStatus
	}
	state.ChannelID = channelID
	state.WorldID = worldID
	state.DungeonID = dungeonID
	state.Level = level
	state.Nation = nation
	state.EntryLimitBase = entryLimitBase
	state.EntryLimitRange = entryLimitRange
	if state.ChatServerID == 0 {
		state.ChatServerID = chatServerID(state.Server)
	}

	pkt := network.NewWriter(charGameStateChange)
	pkt.WriteInt32(state.CharacterID)
	pkt.WriteUint32(state.ChatServerID)
	pkt.WriteUint16(changeFlag)
	pkt.WriteByte(state.PresenceStatus)
	pkt.WriteByte(state.ChannelID)
	pkt.WriteByte(state.WorldID)
	pkt.WriteUint16(state.DungeonID)
	pkt.WriteByte(state.Level)
	pkt.WriteByte(state.Nation)
	pkt.WriteByte(state.EntryLimitBase)
	pkt.WriteByte(state.EntryLimitRange)
	session.Send(pkt)

	if changeFlag&stateChangePresence != 0 {
		refreshBuddyPresence(configuredServer(state), state.CharacterID)
	}
}

func handleBuddyMemberMemoModify(session *network.Session, state *State, reader *network.Reader) {
	if state.CharacterID <= 0 || reader.Size < 18 {
		sendBuddyMemoModifyResult(session, buddy.MemoModifyInvalidParam, 0, "")
		return
	}

	targetID := int32(reader.ReadUint32())
	dataLen := reader.ReadUint16()
	remaining := int(reader.Size) - 16 // header, targetCharIdx and dataLen
	if uint32(targetID) == ^uint32(0) || targetID <= 0 || dataLen < 2 ||
		int(dataLen) > remaining {
		sendBuddyMemoModifyResult(session, buddy.MemoModifyInvalidParam, 0, "")
		return
	}

	memoLen := reader.ReadUint16()
	if memoLen > 50 || int(memoLen) > int(dataLen)-2 {
		sendBuddyMemoModifyResult(session, buddy.MemoModifyInvalidParam, 0, "")
		return
	}
	memo := textcodec.FromClient(reader.ReadBytes(int(memoLen)))
	log.Debugf("ChatNode buddy memo modify: character=%d target=%d memo=%q",
		state.CharacterID, targetID, memo)

	response := buddy.MemoModifyResponse{Result: buddy.MemoModifyInvalidParam}
	var rpcErr error
	if g_RPCHandler == nil {
		rpcErr = rpc.ErrShutdown
	} else {
		rpcErr = g_RPCHandler.Call(rpc.UpdateBuddyMemo, &buddy.MemoModifyRequest{
			Server: configuredServer(state), Character: state.CharacterID,
			Target: targetID, Memo: memo,
		}, &response)
	}
	if rpcErr != nil {
		log.Errorf("ChatNode buddy memo modify failed: character=%d target=%d memo=%q: %s",
			state.CharacterID, targetID, memo, rpcErr)
		response.Result = buddy.MemoModifyDBFail
	}
	if response.Result != buddy.MemoModifyOK {
		log.Warningf("ChatNode buddy memo modify rejected: character=%d target=%d result=%d",
			state.CharacterID, targetID, response.Result)
		sendBuddyMemoModifyResult(session, response.Result, 0, "")
		return
	}

	// WorldSvr echoes the value accepted by the request in the 470 response.
	// Keep the request as the authoritative value if an older RPC peer returns
	// an empty/unset response field; otherwise the client can apply a stale
	// memo even though the database update succeeded.
	if response.Memo != memo {
		log.Warningf("ChatNode buddy memo modify RPC echo mismatch: character=%d target=%d request=%q response=%q",
			state.CharacterID, targetID, memo, response.Memo)
	}
	log.Debugf("ChatNode buddy memo modify result: character=%d target=%d result=%d memo=%q",
		state.CharacterID, targetID, response.Result, memo)
	sendBuddyMemoModifyResult(session, response.Result, targetID, memo)
}

func sendBuddyMemberList(session *network.Session, state *State, reserved uint32) {
	response := buddy.ListResponse{}
	if g_RPCHandler != nil && state.CharacterID > 0 {
		request := buddy.ListRequest{Server: configuredServer(state), Character: state.CharacterID}
		if err := g_RPCHandler.Call(rpc.LoadBuddyList, &request, &response); err != nil {
			log.Errorf("ChatNode buddy list load failed: character=%d: %s", state.CharacterID, err)
		}
	}

	pkt := network.NewWriter(buddyGetMemberList)
	pkt.WriteUint32(0) // charIdx is reserved in S2C_RESULTBUDDYMEMBERLIST.
	pkt.WriteUint32(reserved)
	data := network.NewWriter(0)
	for _, member := range response.Members {
		status, serverID := buddyPresence(member.ID)
		memberState := buddyStateForCharacter(member.ID)
		log.Debugf("ChatNode buddy member presence: owner=%d target=%d status=%d chatSvrId=%08X",
			state.CharacterID, member.ID, status, serverID)
		data.WriteInt32(member.ID)
		data.WriteByte(status)
		data.WriteUint32(serverID)
		data.WriteByte(member.Nation)
		if status != presenceOffline && memberState != nil {
			data.WriteByte(memberState.ChannelID)
			data.WriteByte(memberState.EntryLimitBase)
			data.WriteByte(memberState.EntryLimitRange)
			data.WriteByte(memberState.WorldID)
			data.WriteUint16(memberState.DungeonID)
		} else {
			data.WriteByte(0)   // channelIdx
			data.WriteByte(0)   // entryLimitLevel.base
			data.WriteByte(255) // entryLimitLevel.range
			data.WriteByte(0)   // worldIdx
			data.WriteUint16(0) // dungeonIdx
		}
		data.WriteUint32(member.Group)
		data.WriteUint32(charRemark(member.BattleStyle, member.Nation, member.ClassLevel))
		data.WriteByte(member.Level)
		writeForwardString(data, member.Name, 16)
		writeForwardString(data, member.Memo, 50)
	}
	encoded := dataBytes(data)
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
	session.Send(pkt)
	log.Debugf("ChatNode buddy member list response: character=%d count=%d", state.CharacterID, len(response.Members))
}

func handleBuddyMemberAdd(session *network.Session, state *State, reader *network.Reader) {
	if reader.Size < 14 {
		sendBuddyAddResult(session, buddy.ResultInvalidParam, nil)
		return
	}
	dataLen := reader.ReadUint16()
	if int(dataLen) < 2 || int(dataLen) > int(reader.Size)-12 {
		sendBuddyAddResult(session, buddy.ResultInvalidParam, nil)
		return
	}
	nameLen := reader.ReadUint16()
	if nameLen == 0 || int(nameLen) > int(dataLen)-2 {
		sendBuddyAddResult(session, buddy.ResultInvalidParam, nil)
		return
	}
	targetName := textcodec.FromClient(reader.ReadBytes(int(nameLen)))
	log.Debugf("ChatNode buddy add request: character=%d target=%q", state.CharacterID, targetName)
	if state.CharacterID <= 0 || g_RPCHandler == nil {
		sendBuddyAddResult(session, buddy.ResultInvalidParam, nil)
		return
	}

	serverID := configuredServer(state)
	target := buddy.LookupResponse{}
	if err := g_RPCHandler.Call(rpc.LookupBuddyCharacter, &buddy.LookupRequest{
		Server: serverID,
		Name:   targetName,
	}, &target); err != nil || !target.Found {
		log.Warningf("ChatNode buddy target not found: requester=%d target=%q err=%v", state.CharacterID, targetName, err)
		sendBuddyAddResult(session, buddy.ResultNotExist, nil)
		return
	}
	if target.Character.ID == state.CharacterID {
		sendBuddyAddResult(session, buddy.ResultInvalidParam, nil)
		return
	}
	if _, targetState := buddySession(target.Character.ID); targetState == nil {
		sendBuddyAddResult(session, buddy.ResultTargetOffline, nil)
		return
	}

	if alreadyBuddy(state, target.Character.ID, serverID) {
		sendBuddyAddResult(session, buddy.ResultAlreadyMember, nil)
		return
	}
	if hasBuddyLimit(state.CharacterID, serverID) {
		sendBuddyAddResult(session, buddy.ResultMaxMember, nil)
		return
	}
	if hasBuddyLimit(target.Character.ID, serverID) {
		sendBuddyAddResult(session, buddy.ResultMaxMemberTarget, nil)
		return
	}

	requester := buddy.LookupResponse{}
	if err := g_RPCHandler.Call(rpc.LookupBuddyCharacter, &buddy.LookupRequest{
		Server:      serverID,
		CharacterID: state.CharacterID,
	}, &requester); err != nil || !requester.Found {
		sendBuddyAddResult(session, buddy.ResultInvalidParam, nil)
		return
	}

	pendingBuddyInvites.Lock()
	for _, invite := range pendingBuddyInvites.byTarget {
		if invite.Requester.ID == requester.Character.ID {
			pendingBuddyInvites.Unlock()
			sendBuddyAddResult(session, buddy.ResultInvitingByOther, nil)
			return
		}
	}
	if _, exists := pendingBuddyInvites.byTarget[target.Character.ID]; exists {
		pendingBuddyInvites.Unlock()
		sendBuddyAddResult(session, buddy.ResultInvitingByOther, nil)
		return
	}
	pendingBuddyInvites.byTarget[target.Character.ID] = pendingBuddyInvite{
		Requester: requester.Character,
		Target:    target.Character,
	}
	pendingBuddyInvites.Unlock()

	sendBuddyAddResult(session, buddy.ResultInviting, &target.Character)
	if targetSession, _ := buddySession(target.Character.ID); targetSession != nil {
		sendBuddyAddRequest(targetSession, &requester.Character)
	}
}

func handleBuddyMemberResponse(session *network.Session, state *State, reader *network.Reader) {
	if reader.Size < 15 || state.CharacterID <= 0 {
		sendBuddyResponseResult(session, buddy.ResultInvalidParam, nil)
		return
	}
	requesterID := int32(reader.ReadUint32())
	accepted := reader.ReadByte() != 0

	pendingBuddyInvites.Lock()
	invite, exists := pendingBuddyInvites.byTarget[state.CharacterID]
	if exists && invite.Requester.ID != requesterID {
		exists = false
	}
	if exists {
		delete(pendingBuddyInvites.byTarget, state.CharacterID)
	}
	pendingBuddyInvites.Unlock()
	if !exists {
		sendBuddyResponseResult(session, buddy.ResultInvalidParam, nil)
		return
	}

	requesterSession, _ := buddySession(invite.Requester.ID)
	if !accepted {
		if requesterSession != nil {
			sendBuddyAddResult(requesterSession, buddy.ResultRefuse, nil)
		}
		return
	}

	result := buddy.AddResponse{Result: buddy.ResultInvalidParam}
	if g_RPCHandler == nil || g_RPCHandler.Call(rpc.AddBuddy, &buddy.AddRequest{
		Server:    configuredServer(state),
		Requester: invite.Requester.ID,
		Target:    invite.Target.ID,
	}, &result) != nil {
		result.Result = buddy.ResultDBFail
	}

	if result.Result != buddy.ResultOK {
		sendBuddyResponseResult(session, result.Result, nil)
		if requesterSession != nil {
			sendBuddyAddResult(requesterSession, result.Result, nil)
		}
		return
	}

	sendBuddyResponseResult(session, buddy.ResultOK, &invite.Requester)
	if requesterSession != nil {
		sendBuddyAddResult(requesterSession, buddy.ResultOK, &invite.Target)
	}
	log.Debugf("ChatNode buddy add accepted: requester=%d target=%d", invite.Requester.ID, invite.Target.ID)
	sendBuddyMemberList(session, state, 0)
	if requesterSession != nil {
		if _, requesterState := buddySession(invite.Requester.ID); requesterState != nil {
			sendBuddyMemberList(requesterSession, requesterState, 0)
		}
	}
}

func alreadyBuddy(state *State, targetID int32, serverID byte) bool {
	if g_RPCHandler == nil {
		return false
	}
	response := buddy.ListResponse{}
	if err := g_RPCHandler.Call(rpc.LoadBuddyList, &buddy.ListRequest{
		Server: serverID, Character: state.CharacterID,
	}, &response); err != nil {
		return false
	}
	for _, member := range response.Members {
		if member.ID == targetID {
			return true
		}
	}
	return false
}

func hasBuddyLimit(characterID int32, serverID byte) bool {
	if g_RPCHandler == nil {
		return false
	}
	response := buddy.ListResponse{}
	if err := g_RPCHandler.Call(rpc.LoadBuddyList, &buddy.ListRequest{
		Server: serverID, Character: characterID,
	}, &response); err != nil {
		return false
	}
	return len(response.Members) >= buddy.MaxMembers
}

func sendBuddyAddResult(session *network.Session, result byte, target *buddy.Character) {
	if session == nil {
		return
	}
	pkt := network.NewWriter(buddyMemberAdd)
	pkt.WriteByte(result)
	data := network.NewWriter(0)
	if target != nil && (result == buddy.ResultOK || result == buddy.ResultInviting) {
		writeBuddyAddMember(data, *target, buddyPresenceFor(target.ID))
	}
	encoded := dataBytes(data)
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
	session.Send(pkt)
}

func sendBuddyAddRequest(session *network.Session, requester *buddy.Character) {
	if session == nil || requester == nil {
		return
	}
	pkt := network.NewWriter(buddyMemberRequest)
	pkt.WriteUint32(uint32(requester.ID))
	data := network.NewWriter(0)
	writeForwardString(data, requester.Name, 16)
	data.WriteByte(requester.Level)
	data.WriteUint32(charRemark(requester.BattleStyle, requester.Nation, requester.ClassLevel))
	encoded := dataBytes(data)
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
	session.Send(pkt)
}

func sendBuddyResponseResult(session *network.Session, result byte, requester *buddy.Character) {
	if session == nil {
		return
	}
	pkt := network.NewWriter(buddyMemberResponse)
	pkt.WriteByte(result)
	data := network.NewWriter(0)
	if requester != nil && result == buddy.ResultOK {
		writeBuddyAddMember(data, *requester, buddyPresenceFor(requester.ID))
	}
	encoded := dataBytes(data)
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
	session.Send(pkt)
}

func sendBuddyMemoModifyResult(session *network.Session, result byte, targetID int32, memo string) {
	if session == nil {
		return
	}
	pkt := network.NewWriter(buddyMemberMemoModify)
	if result == buddy.MemoModifyOK && targetID > 0 {
		pkt.WriteUint32(uint32(targetID))
	} else {
		pkt.WriteUint32(^uint32(0))
	}
	pkt.WriteByte(result)
	data := network.NewWriter(0)
	if result == buddy.MemoModifyOK {
		writeForwardString(data, memo, 50)
	}
	encoded := dataBytes(data)
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
	session.Send(pkt)
}

func sendBuddyDeleteResult(session *network.Session, result byte, targetID int32) {
	if session == nil {
		return
	}
	pkt := network.NewWriter(buddyMemberDelete)
	if result == buddy.DeleteOK && targetID > 0 {
		pkt.WriteUint32(uint32(targetID))
	} else {
		pkt.WriteUint32(^uint32(0))
	}
	pkt.WriteByte(result)
	session.Send(pkt)
}

func sendBuddyDeleteNotification(session *network.Session, deleterID, targetID int32) {
	if session == nil || deleterID <= 0 || targetID <= 0 {
		return
	}
	pkt := network.NewWriter(buddyMemberDeleteNotify)
	pkt.WriteUint32(uint32(deleterID))
	pkt.WriteUint32(uint32(targetID))
	session.Send(pkt)
}

func sendEquipmentViewResult(session *network.Session, result, targetID int32,
	targetBattleStyle byte, data []byte) {
	if session == nil {
		return
	}
	if len(data)%equipmentViewDataSize != 0 {
		data = data[:len(data)-len(data)%equipmentViewDataSize]
	}
	count := len(data) / equipmentViewDataSize
	if count > equipmentViewMaxCount {
		count = equipmentViewMaxCount
		data = data[:count*equipmentViewDataSize]
	}

	pkt := network.NewWriter(equipmentViewNotifyResponse)
	pkt.WriteInt32(result)
	pkt.WriteInt32(targetID)
	pkt.WriteByte(targetBattleStyle)
	pkt.WriteByte(byte(count))
	pkt.WriteBytes(data)
	session.Send(pkt)
}

func writeBuddyAddMember(pkt *network.Writer, member buddy.Character, presence byte) {
	pkt.WriteInt32(member.ID)
	pkt.WriteUint32(buddy.DefaultGroup)
	_, serverID := buddyPresence(member.ID)
	pkt.WriteUint32(serverID)
	pkt.WriteByte(member.Nation)
	pkt.WriteByte(0) // channelIdx
	pkt.WriteByte(0) // worldIdx
	pkt.WriteUint32(charRemark(member.BattleStyle, member.Nation, member.ClassLevel))
	pkt.WriteByte(member.Level)
	pkt.WriteByte(presence)
	writeForwardString(pkt, member.Name, 16)
}

func writeForwardString(pkt *network.Writer, value string, max int) {
	encoded := textcodec.ToClient(value)
	if len(encoded) > max {
		encoded = encoded[:max]
	}
	pkt.WriteUint16(uint16(len(encoded)))
	pkt.WriteBytes(encoded)
}

func dataBytes(pkt *network.Writer) []byte {
	// network.Writer serializes magic, size and type: 2 + 2 + 2 bytes.
	// The Reader additionally handles a four-byte checksum on incoming
	// packets, but that checksum is not part of Writer's output buffer.
	return pkt.Finalize()[6:]
}

func charRemark(battleStyle, nation, classLevel byte) uint32 {
	return uint32(battleStyle&7) | uint32(nation&7)<<3 | uint32(classLevel&31)<<6
}

func buddyPresenceFor(characterID int32) byte {
	status, _ := buddyPresence(characterID)
	return status
}

func buddySessionOnline(characterID int32) bool {
	_, state := buddySession(characterID)
	return state != nil
}

func buddyPresence(characterID int32) (byte, uint32) {
	_, state := buddySession(characterID)
	if state == nil || !state.LoggedIn {
		return presenceOffline, 0
	}
	status := state.PresenceStatus
	if status == 0 || status >= presenceStatusEnd {
		status = presenceOnline
	}
	if state.ChatServerID == 0 {
		state.ChatServerID = chatServerID(state.Server)
	}
	return status, state.ChatServerID
}

func chatServerID(serverID byte) uint32 {
	return uint32(chatNodeServerType)<<16 | uint32(serverID)<<8 | chatNodeGroupIndex
}

func buddyStateForCharacter(characterID int32) *State {
	if g_NetworkManager == nil || characterID <= 0 {
		return nil
	}
	var disconnectedState *State
	for _, session := range g_NetworkManager.Sessions() {
		state, ok := session.DataEx.(*State)
		if !ok || state.CharacterID != characterID || session.IsClosed() {
			continue
		}
		if state.LoggedIn {
			return state
		}
		if disconnectedState == nil {
			disconnectedState = state
		}
	}
	return disconnectedState
}

func buddySession(characterID int32) (*network.Session, *State) {
	if g_NetworkManager == nil || characterID <= 0 {
		return nil, nil
	}
	for _, session := range g_NetworkManager.Sessions() {
		state, ok := session.DataEx.(*State)
		if ok && state.LoggedIn && state.CharacterID == characterID && !session.IsClosed() {
			return session, state
		}
	}
	return nil, nil
}

func handleLogout(session *network.Session, state *State) {
	// ChatNode sends S2C_CHATLOGOUT with the same opcode as the request.
	// The client waits for this acknowledgement before creating a new
	// ChatNode connection during a reconnect.
	session.Send(network.NewWriter(chatLogout))

	characterID := state.CharacterID
	serverID := configuredServer(state)
	state.PresenceStatus = presenceOffline
	state.LoggedIn = false
	NotifyBuddyPresence(serverID, characterID)
	ClearEquipmentViewRequests(characterID)

	state.CharacterID = 0
	state.Authorized = false

	// Match WorldSvr's CloseWait(2000): allow the acknowledgement to reach
	// the client, then close the old ChatNode session.
	time.AfterFunc(2*time.Second, session.Close)
}

func handleConnect(session *network.Session) {
	pkt := network.NewWriter(connect2Svr)
	pkt.WriteUint32(session.Encryption.Key.Seed2nd)
	pkt.WriteUint32(session.AuthKey)
	pkt.WriteUint16(session.UserIdx)
	pkt.WriteUint16(session.Encryption.RecvXorKeyIdx)
	session.Send(pkt)
}

func handleAuth(session *network.Session, reader *network.Reader, state *State) {
	if reader.Size < 16 {
		log.Warningf("Invalid ChatNode auth packet size: %d", reader.Size)
		return
	}

	state.CharacterID = reader.ReadInt32()
	_ = reader.ReadUint16() // account/password data length
	state.Authorized = true
	state.GuildNo = 0
	if g_RPCHandler != nil && state.CharacterID > 0 {
		response := buddy.LookupResponse{}
		if err := g_RPCHandler.Call(rpc.LookupBuddyCharacter, &buddy.LookupRequest{
			Server: configuredServer(state), CharacterID: state.CharacterID,
		}, &response); err == nil && response.Found {
			state.GuildNo = response.Character.GuildNo
		}
	}

	pkt := network.NewWriter(chatAuthAccount)
	pkt.WriteInt32(state.CharacterID)
	pkt.WriteUint32(state.GuildNo)
	pkt.WriteByte(0) // CHATAUTH_RESULT_OK
	session.Send(pkt)
}

func handleGuildCreate(session *network.Session, state *State, reader *network.Reader) {
	if state.CharacterID <= 0 || reader.Size < 14 {
		sendGuildCreateResult(session, 0, guild.CreateInvalidParam)
		return
	}

	dataLen := reader.ReadUint16()
	if dataLen < 2 || int(dataLen) > int(reader.Size)-12 {
		sendGuildCreateResult(session, 0, guild.CreateInvalidParam)
		return
	}
	nameLen := reader.ReadUint16()
	if nameLen == 0 || nameLen > guild.MaxNameBytes || int(nameLen) > int(dataLen)-2 {
		sendGuildCreateResult(session, 0, guild.CreateInvalidParam)
		return
	}

	name := textcodec.FromClient(reader.ReadBytes(int(nameLen)))
	log.Debugf("ChatNode guild create request: character=%d name=%q", state.CharacterID, name)
	response := guild.CreateResponse{Result: guild.CreateInvalidParam}
	var rpcErr error
	if g_RPCHandler == nil {
		rpcErr = rpc.ErrShutdown
	} else {
		rpcErr = g_RPCHandler.Call(rpc.CreateGuild, &guild.CreateRequest{
			Server: configuredServer(state), Character: state.CharacterID, Name: name,
		}, &response)
	}
	if rpcErr != nil {
		log.Errorf("ChatNode guild create failed: character=%d name=%q: %s",
			state.CharacterID, name, rpcErr)
		response.GuildNo = 0
		response.Result = guild.CreateDBFail
	}
	if response.Result == guild.CreateOK && response.GuildNo != 0 {
		state.GuildNo = response.GuildNo
	}
	sendGuildCreateResult(session, response.GuildNo, response.Result)
	log.Debugf("ChatNode guild create result: character=%d guild=%d result=%d",
		state.CharacterID, response.GuildNo, response.Result)
}

func sendGuildCreateResult(session *network.Session, guildNo uint32, result byte) {
	if session == nil {
		return
	}
	pkt := network.NewWriter(guildCreate)
	pkt.WriteUint32(guildNo)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func handleLogin(session *network.Session, reader *network.Reader, state *State) {
	if reader.Size < 11 {
		log.Warningf("Invalid ChatNode login packet size: %d", reader.Size)
		return
	}

	// The RU client uses the extended C2S_CHATLOGIN layout:
	// charIdx, guildNo, serverNationIdx, channelIdx, worldIdx, charRemark,
	// charLevel, entryLimitLevel and a variable name field. Older builds with
	// FIX_CHATNODE_LOGIN send only channelIdx; the character was already bound
	// by CHATAUTHACCOUNT in that variant.
	if reader.Size >= 28 {
		state.CharacterID = reader.ReadInt32()
		_ = reader.ReadInt32() // guild number
		state.Nation = reader.ReadByte()
		state.ChannelID = reader.ReadByte()
		state.WorldID = reader.ReadByte()
		_ = reader.ReadUint32() // char remark is loaded from World DB for lists
		state.Level = reader.ReadByte()
		state.EntryLimitBase = reader.ReadByte()
		state.EntryLimitRange = reader.ReadByte()
	} else {
		state.ChannelID = reader.ReadByte()
	}
	if state.CharacterID <= 0 {
		log.Warningf("ChatNode login has no authenticated character: packetSize=%d", reader.Size)
		return
	}
	state.LoggedIn = true
	state.ChatServerID = chatServerID(state.Server)
	state.PresenceStatus = presenceOnline

	pkt := network.NewWriter(chatLogin)
	pkt.WriteByte(0) // CHATLOGIN_RESULT_OK
	session.Send(pkt)
	NotifyBuddyPresence(configuredServer(state), state.CharacterID)
}

func sendMailList(session *network.Session, state *State, packetType int) {
	response := mail.ListResponse{}
	if g_RPCHandler != nil && state.CharacterID > 0 {
		request := mail.ListRequest{Server: configuredServer(state), Character: state.CharacterID}
		if err := g_RPCHandler.Call(rpc.LoadMailLists, &request, &response); err != nil {
			log.Errorf("ChatNode mail list load failed: character=%d: %s", state.CharacterID, err)
		}
	}

	if packetType == mailReceivedList {
		writeReceivedList(session, response.Received)
		return
	}
	writeSentList(session, response.Sent)
}

func sendMailUpdate(session *network.Session, state *State, lastMailID int32) {
	response := mail.ListResponse{}
	if g_RPCHandler != nil && state.CharacterID > 0 {
		request := mail.ListRequest{Server: configuredServer(state), Character: state.CharacterID, LastMailID: lastMailID}
		if err := g_RPCHandler.Call(rpc.LoadMailLists, &request, &response); err != nil {
			log.Errorf("ChatNode new-mail load failed: character=%d: %s", state.CharacterID, err)
		}
	}

	pkt := network.NewWriter(mailUpdateList)
	pkt.WriteBool(true) // isLast
	pkt.WriteInt32(0)   // sequence
	count := len(response.New)
	if count > 5 {
		count = 5
	}
	pkt.WriteByte(byte(count))
	for _, summary := range response.New[:count] {
		writeReceivedSummary(pkt, summary)
		detail := mail.Detail{}
		if g_RPCHandler != nil {
			request := mail.DetailRequest{
				Server:    configuredServer(state),
				Character: state.CharacterID,
				MailID:    summary.ID,
			}
			result := mail.DetailResponse{}
			if err := g_RPCHandler.Call(rpc.LoadMailDetail, &request, &result); err == nil && result.Result {
				detail = result.Detail
			}
		}
		writeMailDetail(pkt, detail)
	}
	session.Send(pkt)
	log.Debugf("ChatNode mail update response: count=%d", count)
}

func writeReceivedList(session *network.Session, summaries []mail.Summary) {
	for offset := 0; offset < len(summaries) || offset == 0; offset += 20 {
		end := offset + 20
		if end > len(summaries) {
			end = len(summaries)
		}
		pkt := network.NewWriter(mailReceivedList)
		pkt.WriteBool(end == len(summaries))
		pkt.WriteInt32(int32(offset / 20))
		pkt.WriteByte(byte(end - offset))
		for _, summary := range summaries[offset:end] {
			writeReceivedSummary(pkt, summary)
		}
		session.Send(pkt)
		if end == len(summaries) {
			break
		}
	}
	log.Debugf("ChatNode mail list response: type=%d count=%d", mailReceivedList, len(summaries))
}

func writeSentList(session *network.Session, summaries []mail.Summary) {
	for offset := 0; offset < len(summaries) || offset == 0; offset += 20 {
		end := offset + 20
		if end > len(summaries) {
			end = len(summaries)
		}
		pkt := network.NewWriter(mailSentList)
		pkt.WriteBool(end == len(summaries))
		pkt.WriteInt32(int32(offset / 20))
		pkt.WriteByte(byte(end - offset))
		for _, summary := range summaries[offset:end] {
			writeSentSummary(pkt, summary)
		}
		session.Send(pkt)
		if end == len(summaries) {
			break
		}
	}
	log.Debugf("ChatNode mail list response: type=%d count=%d", mailSentList, len(summaries))
}

func writeReceivedSummary(pkt *network.Writer, summary mail.Summary) {
	pkt.WriteInt32(summary.ID)
	pkt.WriteByte(summary.Type)
	pkt.WriteInt64(summary.DeliveryTime)
	pkt.WriteByte(summary.State)
	writeFixedString(pkt, summary.SenderName, 17)
	writeFixedString(pkt, summary.Title, 41)
}

func writeSentSummary(pkt *network.Writer, summary mail.Summary) {
	pkt.WriteInt32(summary.ID)
	pkt.WriteInt64(summary.DeliveryTime)
	writeFixedString(pkt, summary.ReceiverName, 17)
	writeFixedString(pkt, summary.Title, 41)
}

func writeMailDetail(pkt *network.Writer, detail mail.Detail) {
	pkt.WriteInt64(int64(detail.Alz))
	pkt.WriteUint32(detail.Item.Kind)
	pkt.WriteInt32(detail.Item.Option)
	pkt.WriteUint32(detail.Item.Expire)
	content := textcodec.ToClient(detail.Content)
	if len(content) > mail.MaxContent {
		content = content[:mail.MaxContent]
	}
	pkt.WriteUint16(uint16(len(content)))
	pkt.WriteBytes(content)
	pkt.WriteByte(0)
}

func writeFixedString(pkt *network.Writer, value string, size int) {
	data := make([]byte, size)
	copy(data, textcodec.ToClient(value))
	pkt.WriteBytes(data)
}

func configuredServer(state *State) byte {
	if state.Server == 0 {
		return 1
	}
	return state.Server
}
