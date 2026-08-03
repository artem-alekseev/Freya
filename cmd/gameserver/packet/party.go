package packet

import (
	"strings"
	"sync/atomic"

	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	partyInviteRequestMinSize  = 16 // header + type + character index + channel
	partyInviteResultSize      = 19 // header + inviting character index + channel + BOOL
	partyInviteCancelSize      = 15 // header + invited character index + channel
	partyKickoutRequestSize    = 15 // header + kicked character index + channel
	partyLeaderChangeSize      = 14 // header + new leader character index
	partyAuthChangeRequestSize = 14 // header + BOOL
	partyLootingTypeSize       = 18 // header + normal looting enum + owner looting enum
	partySearchRegistSize      = 51 // header + max member count + title[40]
	partyMaxNameLength         = 17 // MAX_CHARNAMELEN, including the terminator
	partySearchTitleSize       = 40 // MAX_PARTYSEARCH_TITLE
	partyMemberDefaultDataSize = 39 // packed PartyMemberDefaultData with assistantIdx
	partyMemberTotalDataSize   = 75 // packed PartyMemberTotalData with assistant fields
	partySearchEntrySize       = 72 // sizeof(PartySearchCtxList), including C++ alignment padding
	partyNormalLootingFree     = 1
	partyNormalLootingOrdered  = 2
	partyNormalLootingLeader   = 3
	partyOwnerLootingDice      = 1
	partyOwnerLootingAuction   = 2
	partySearchMinLevel        = 10

	partyInviteOK             byte = 0x00
	partyInviteNotExist       byte = 0x01
	partyInviteBusy           byte = 0x02
	partyInviteMaxMembers     byte = 0x07
	partyInviteAlreadyMember  byte = 0x08
	partyInviteError          byte = 0x0B
	partyInviteAnswerOK       byte = 0x00
	partyInviteAnswerRejected byte = 0x01
	partyInviteAnswerFailed   byte = 0x02
)

var partySequence int32 = 1000

// PartyInvite handles CSC_NEW_PARTYINVITE (2011). The target is normally
// identified by character id; the name is retained for the name-invite path.
func PartyInvite(session *network.Session, reader *network.Reader) {
	if reader.Size < partyInviteRequestMinSize {
		log.Warningf("[PARTYINVITE] invalid packet size: %d", reader.Size)
		sendPartyInviteResponse(session, partyInviteError, context.PartyMember{})
		return
	}

	inviteType := reader.ReadByte()
	targetCharacterID := reader.ReadInt32()
	targetChannel := reader.ReadByte()
	targetName := ""
	if reader.Size >= partyInviteRequestMinSize+partyMaxNameLength {
		targetName = decodePartyName(reader.ReadString(partyMaxNameLength))
	}

	senderContext, err := context.Parse(session)
	if err != nil {
		sendPartyInviteResponse(session, partyInviteError, context.PartyMember{})
		return
	}

	senderContext.Mutex.RLock()
	senderMember, senderOK := partyMemberFromContext(session, senderContext)
	senderParty := clonePartyState(senderContext.Party)
	senderInvite := senderContext.PartyInvite
	senderContext.Mutex.RUnlock()
	if !senderOK {
		sendPartyInviteResponse(session, partyInviteError, context.PartyMember{})
		return
	}

	if inviteType != 1 && inviteType != 2 {
		sendPartyInviteResponse(session, partyInviteError, context.PartyMember{})
		return
	}
	if inviteType == 1 && targetCharacterID <= 0 {
		sendPartyInviteResponse(session, partyInviteNotExist, context.PartyMember{})
		return
	}

	targetSession := findPartySession(targetCharacterID, targetName)
	if targetSession == nil || targetSession.UserIdx == session.UserIdx {
		sendPartyInviteResponse(session, partyInviteNotExist, context.PartyMember{})
		return
	}
	targetContext, err := context.Parse(targetSession)
	if err != nil {
		sendPartyInviteResponse(session, partyInviteNotExist, context.PartyMember{})
		return
	}

	targetContext.Mutex.RLock()
	targetMember, targetOK := partyMemberFromContext(targetSession, targetContext)
	targetParty := clonePartyState(targetContext.Party)
	targetInvite := targetContext.PartyInvite
	targetContext.Mutex.RUnlock()
	if !targetOK {
		sendPartyInviteResponse(session, partyInviteNotExist, context.PartyMember{})
		return
	}

	if senderInvite.Status != context.PartyInviteNone ||
		targetInvite.Status != context.PartyInviteNone {
		sendPartyInviteResponse(session, partyInviteBusy, targetMember)
		return
	}
	if senderParty.ID != 0 && targetParty.ID != 0 {
		sendPartyInviteResponse(session, partyInviteAlreadyMember, targetMember)
		return
	}
	if (senderParty.ID != 0 && len(senderParty.Members) >= context.PartyMaxMembers) ||
		(targetParty.ID != 0 && len(targetParty.Members) >= context.PartyMaxMembers) {
		sendPartyInviteResponse(session, partyInviteMaxMembers, targetMember)
		return
	}

	lockPartyContexts(session, senderContext, targetSession, targetContext)
	if senderContext.PartyInvite.Status != context.PartyInviteNone ||
		targetContext.PartyInvite.Status != context.PartyInviteNone {
		unlockPartyContexts(session, senderContext, targetSession, targetContext)
		sendPartyInviteResponse(session, partyInviteBusy, targetMember)
		return
	}
	if senderContext.Party.ID != 0 && targetContext.Party.ID != 0 {
		unlockPartyContexts(session, senderContext, targetSession, targetContext)
		sendPartyInviteResponse(session, partyInviteAlreadyMember, targetMember)
		return
	}
	if (senderContext.Party.ID != 0 && len(senderContext.Party.Members) >= context.PartyMaxMembers) ||
		(targetContext.Party.ID != 0 && len(targetContext.Party.Members) >= context.PartyMaxMembers) {
		unlockPartyContexts(session, senderContext, targetSession, targetContext)
		sendPartyInviteResponse(session, partyInviteMaxMembers, targetMember)
		return
	}

	senderContext.PartyInvite = context.PartyInviteState{
		Status:              context.PartyInviteOutgoing,
		OpponentUserIdx:     targetSession.UserIdx,
		OpponentCharacterID: targetMember.CharacterID,
		Channel:             targetChannel,
	}
	targetContext.PartyInvite = context.PartyInviteState{
		Status:              context.PartyInviteIncoming,
		OpponentUserIdx:     session.UserIdx,
		OpponentCharacterID: senderMember.CharacterID,
		Channel:             senderMember.Channel,
	}
	unlockPartyContexts(session, senderContext, targetSession, targetContext)

	// The requester receives the result with the target's identity. The
	// target receives a separate notification containing the inviter.
	targetMember.Channel = targetChannel
	sendPartyInviteResponse(session, partyInviteOK, targetMember)
	sendPartyInviteNotification(targetSession, senderMember)
	log.Debugf("[PARTYINVITE] sent: inviter=%d target=%d", senderMember.CharacterID, targetMember.CharacterID)
}

// PartyInviteResult handles CSC_NEW_PARTYINVITE_RESULT (2013).
func PartyInviteResult(session *network.Session, reader *network.Reader) {
	if reader.Size < partyInviteResultSize {
		log.Warningf("[PARTYINVITE_RESULT] invalid packet size: %d", reader.Size)
		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		return
	}

	invitingCharacterID := reader.ReadInt32()
	_ = reader.ReadByte() // inviting channel; the reserved invitation state is authoritative
	accepted := reader.ReadInt32() != 0

	targetContext, err := context.Parse(session)
	if err != nil {
		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		return
	}
	targetContext.Mutex.RLock()
	targetInvite := targetContext.PartyInvite
	targetContext.Mutex.RUnlock()
	if targetInvite.Status != context.PartyInviteIncoming ||
		targetInvite.OpponentCharacterID != invitingCharacterID {
		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		return
	}

	inviterSession := g_NetworkManager.GetSession(targetInvite.OpponentUserIdx)
	if inviterSession == nil {
		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		return
	}
	inviterContext, err := context.Parse(inviterSession)
	if err != nil {
		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		return
	}

	lockPartyContexts(inviterSession, inviterContext, session, targetContext)
	valid := inviterContext.PartyInvite.Status == context.PartyInviteOutgoing &&
		inviterContext.PartyInvite.OpponentUserIdx == session.UserIdx &&
		targetContext.PartyInvite.Status == context.PartyInviteIncoming &&
		targetContext.PartyInvite.OpponentUserIdx == inviterSession.UserIdx &&
		inviterContext.Char != nil && targetContext.Char != nil &&
		inviterContext.Char.Id == invitingCharacterID
	if !valid {
		unlockPartyContexts(inviterSession, inviterContext, session, targetContext)
		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		return
	}

	if !accepted {
		inviterContext.PartyInvite = context.PartyInviteState{}
		targetContext.PartyInvite = context.PartyInviteState{}
		unlockPartyContexts(inviterSession, inviterContext, session, targetContext)

		sendPartyInviteAnswer(session, partyInviteAnswerRejected)
		sendPartyInviteResult(inviterSession, false)
		log.Debugf("[PARTYINVITE_RESULT] rejected: inviter=%d target=%d",
			invitingCharacterID, targetInvite.OpponentCharacterID)
		return
	}

	party, result := makePartyState(inviterSession, inviterContext, session, targetContext)
	if result != partyInviteOK {
		inviterContext.PartyInvite = context.PartyInviteState{}
		targetContext.PartyInvite = context.PartyInviteState{}
		unlockPartyContexts(inviterSession, inviterContext, session, targetContext)

		sendPartyInviteAnswer(session, partyInviteAnswerFailed)
		sendPartyInviteResult(inviterSession, false)
		return
	}
	inviterContext.PartyInvite = context.PartyInviteState{}
	targetContext.PartyInvite = context.PartyInviteState{}
	unlockPartyContexts(inviterSession, inviterContext, session, targetContext)

	sendPartyInviteAnswer(session, partyInviteAnswerOK)
	sendPartyInviteResult(inviterSession, true)
	sessions := applyPartyState(party)
	for _, memberSession := range sessions {
		sendPartyStats(memberSession, party)
	}
	sendPartyMemberStatsToMembers(party)
	log.Debugf("[PARTYINVITE_RESULT] accepted: party=%d leader=%d members=%d",
		party.ID, party.LeaderCharacterID, len(party.Members))
}

// PartyInviteCancel handles CSC_NEW_PARTYINVITE_CANCEL (2017).
func PartyInviteCancel(session *network.Session, reader *network.Reader) {
	if reader.Size < partyInviteCancelSize {
		log.Warningf("[PARTYINVITE_CANCEL] invalid packet size: %d", reader.Size)
		sendPartyInviteCancelResult(session, false)
		return
	}
	_ = reader.ReadInt32() // invited character; state is authoritative
	_ = reader.ReadByte()  // invited channel

	currentContext, err := context.Parse(session)
	if err != nil {
		sendPartyInviteCancelResult(session, false)
		return
	}
	currentContext.Mutex.RLock()
	invite := currentContext.PartyInvite
	currentContext.Mutex.RUnlock()
	if invite.Status != context.PartyInviteOutgoing {
		sendPartyInviteCancelResult(session, false)
		return
	}

	peerSession := g_NetworkManager.GetSession(invite.OpponentUserIdx)
	if peerSession == nil {
		currentContext.Mutex.Lock()
		currentContext.PartyInvite = context.PartyInviteState{}
		currentContext.Mutex.Unlock()
		sendPartyInviteCancelResult(session, true)
		return
	}
	peerContext, err := context.Parse(peerSession)
	if err != nil {
		sendPartyInviteCancelResult(session, false)
		return
	}

	lockPartyContexts(session, currentContext, peerSession, peerContext)
	valid := currentContext.PartyInvite.Status == context.PartyInviteOutgoing &&
		peerContext.PartyInvite.Status == context.PartyInviteIncoming &&
		peerContext.PartyInvite.OpponentUserIdx == session.UserIdx
	if valid {
		currentContext.PartyInvite = context.PartyInviteState{}
		peerContext.PartyInvite = context.PartyInviteState{}
	}
	unlockPartyContexts(session, currentContext, peerSession, peerContext)

	sendPartyInviteCancelResult(session, valid)
	if valid {
		sendPartyInviteCancelNotification(peerSession)
	}
}

// PartyLeave handles CSC_NEW_PARTYLEAVE (2019).
func PartyLeave(session *network.Session, _ *network.Reader) {
	sendPartyLeaveResult(session, LeaveParty(session))
}

// LeaveParty removes a character from its current party. It is also used by
// the lobby/disconnect lifecycle, where no response is sent to the leaving
// client because the session is already leaving the world.
func LeaveParty(session *network.Session) bool {
	currentContext, err := context.Parse(session)
	if err != nil {
		return false
	}

	currentContext.Mutex.RLock()
	party := clonePartyState(currentContext.Party)
	currentMember, ok := partyMemberFromContext(session, currentContext)
	currentContext.Mutex.RUnlock()
	if party.ID == 0 || !ok {
		return false
	}

	remainingCapacity := len(party.Members)
	if remainingCapacity > 0 {
		remainingCapacity--
	}
	remaining := make([]context.PartyMember, 0, remainingCapacity)
	for _, member := range party.Members {
		if member.UserIdx != session.UserIdx {
			remaining = append(remaining, member)
		}
	}

	currentContext.Mutex.Lock()
	currentContext.Party = context.PartyState{}
	currentContext.PartyInvite = context.PartyInviteState{}
	currentContext.Mutex.Unlock()

	if len(remaining) == 0 {
		return true
	}
	if len(remaining) == 1 {
		remainingSession := g_NetworkManager.GetSession(remaining[0].UserIdx)
		if remainingSession != nil {
			clearPartyState(remainingSession)
			sendPartyMemberOut(remainingSession, currentMember.CharacterID)
		}
		log.Debugf("[PARTYLEAVE] character=%d disbanded party=%d; one member remained",
			currentMember.CharacterID, party.ID)
		return true
	}

	party.Members = remaining
	if party.LeaderUserIdx == session.UserIdx {
		party.LeaderUserIdx = remaining[0].UserIdx
		party.LeaderCharacterID = remaining[0].CharacterID
	}

	remainingSessions := applyPartyState(party)
	for _, memberSession := range remainingSessions {
		sendPartyMemberOut(memberSession, currentMember.CharacterID)
		sendPartyStats(memberSession, party)
	}
	sendPartyMemberStatsToMembers(party)
	log.Debugf("[PARTYLEAVE] character=%d party=%d remaining=%d",
		currentMember.CharacterID, party.ID, len(remaining))
	return true
}

// PartyKickout handles CSC_NEW_PARTY_KICKOUT (2021). Only the party leader
// can remove another member.
func PartyKickout(session *network.Session, reader *network.Reader) {
	if reader.Size < partyKickoutRequestSize {
		log.Warningf("[PARTYKICKOUT] invalid packet size: %d", reader.Size)
		sendPartyKickoutResult(session, false)
		return
	}

	kickedCharacterID := reader.ReadInt32()
	_ = reader.ReadByte() // kicked channel

	currentContext, err := context.Parse(session)
	if err != nil {
		sendPartyKickoutResult(session, false)
		return
	}
	currentContext.Mutex.RLock()
	party := clonePartyState(currentContext.Party)
	currentContext.Mutex.RUnlock()

	if party.ID == 0 || party.LeaderUserIdx != session.UserIdx {
		sendPartyKickoutResult(session, false)
		return
	}

	kicked, found := partyMemberByCharacterID(party, kickedCharacterID)
	if !found || kicked.UserIdx == session.UserIdx {
		sendPartyKickoutResult(session, false)
		return
	}

	remaining := make([]context.PartyMember, 0, len(party.Members)-1)
	for _, member := range party.Members {
		if member.UserIdx != kicked.UserIdx {
			remaining = append(remaining, member)
		}
	}

	// WorldSvr broadcasts the kick notification before applying the removal,
	// so the kicked client also receives the packet and closes its party UI.
	for _, member := range party.Members {
		memberSession := g_NetworkManager.GetSession(member.UserIdx)
		if memberSession != nil {
			sendPartyKickoutNotification(memberSession, kickedCharacterID)
		}
	}

	kickedSession := g_NetworkManager.GetSession(kicked.UserIdx)
	if kickedSession != nil {
		clearPartyState(kickedSession)
	}

	if len(remaining) <= 1 {
		if len(remaining) == 1 {
			remainingSession := g_NetworkManager.GetSession(remaining[0].UserIdx)
			if remainingSession != nil {
				clearPartyState(remainingSession)
			}
		}
		log.Debugf("[PARTYKICKOUT] character=%d disbanded party=%d; one member remained",
			kickedCharacterID, party.ID)
		sendPartyKickoutResult(session, true)
		return
	}

	party.Members = remaining
	remainingSessions := applyPartyState(party)
	for _, memberSession := range remainingSessions {
		sendPartyStats(memberSession, party)
	}
	sendPartyMemberStatsToMembers(party)
	sendPartyKickoutResult(session, true)
	log.Debugf("[PARTYKICKOUT] character=%d party=%d remaining=%d",
		kickedCharacterID, party.ID, len(remaining))
}

// PartyLeaderChange handles CSC_NEW_PARTYLEADER_CHANGE (2023).
func PartyLeaderChange(session *network.Session, reader *network.Reader) {
	if reader.Size < partyLeaderChangeSize {
		log.Warningf("[PARTYLEADERCHANGE] invalid packet size: %d", reader.Size)
		sendPartyLeaderChangeResult(session, false)
		return
	}

	newLeaderCharacterID := reader.ReadInt32()
	currentContext, err := context.Parse(session)
	if err != nil {
		sendPartyLeaderChangeResult(session, false)
		return
	}
	currentContext.Mutex.RLock()
	party := clonePartyState(currentContext.Party)
	currentContext.Mutex.RUnlock()

	if party.ID == 0 || party.LeaderUserIdx != session.UserIdx {
		sendPartyLeaderChangeResult(session, false)
		return
	}
	newLeader, found := partyMemberByCharacterID(party, newLeaderCharacterID)
	if !found || newLeader.UserIdx == session.UserIdx {
		sendPartyLeaderChangeResult(session, false)
		return
	}

	party.LeaderUserIdx = newLeader.UserIdx
	party.LeaderCharacterID = newLeader.CharacterID
	sessions := applyPartyState(party)
	for _, memberSession := range sessions {
		sendPartyLeaderChangeNotification(memberSession, newLeader.CharacterID)
	}
	sendPartyLeaderChangeResult(session, true)
	log.Debugf("[PARTYLEADERCHANGE] party=%d leader=%d", party.ID, newLeader.CharacterID)
}

// PartyAuthChange handles CSC_NEW_PARTYAUTH_CHANGE (2025).
func PartyAuthChange(session *network.Session, reader *network.Reader) {
	if reader.Size < partyAuthChangeRequestSize {
		log.Warningf("[PARTYAUTHCHANGE] invalid packet size: %d", reader.Size)
		sendPartyAuthChangeResult(session, false)
		return
	}

	leaderOnly := reader.ReadInt32() != 0
	currentContext, err := context.Parse(session)
	if err != nil {
		sendPartyAuthChangeResult(session, false)
		return
	}
	currentContext.Mutex.RLock()
	party := clonePartyState(currentContext.Party)
	currentContext.Mutex.RUnlock()
	if party.ID == 0 || party.LeaderUserIdx != session.UserIdx {
		sendPartyAuthChangeResult(session, false)
		return
	}

	party.LeaderOnlyInviteAuthority = leaderOnly
	sessions := applyPartyState(party)
	for _, memberSession := range sessions {
		sendPartyAuthChangeNotification(memberSession, leaderOnly)
	}
	sendPartyAuthChangeResult(session, true)
}

// PartyLootingType handles CSC_PARTY_LOOTING_TYPE (2033).
func PartyLootingType(session *network.Session, reader *network.Reader) {
	if reader.Size < partyLootingTypeSize {
		log.Warningf("[PARTYLOOTING] invalid packet size: %d", reader.Size)
		sendPartyLootingTypeResult(session, false)
		return
	}

	normal := reader.ReadInt32()
	owner := reader.ReadInt32()
	if normal < partyNormalLootingFree || normal > partyNormalLootingLeader ||
		owner < partyOwnerLootingDice || owner > partyOwnerLootingAuction {
		sendPartyLootingTypeResult(session, false)
		return
	}

	currentContext, err := context.Parse(session)
	if err != nil {
		sendPartyLootingTypeResult(session, false)
		return
	}
	currentContext.Mutex.RLock()
	party := clonePartyState(currentContext.Party)
	currentContext.Mutex.RUnlock()
	if party.ID == 0 || party.LeaderUserIdx != session.UserIdx {
		sendPartyLootingTypeResult(session, false)
		return
	}

	party.NormalLootingType = byte(normal)
	party.OwnerLootingType = byte(owner)
	sessions := applyPartyState(party)
	for _, memberSession := range sessions {
		sendPartyLootingTypeNotification(memberSession, party.NormalLootingType, party.OwnerLootingType)
	}
	sendPartyLootingTypeResult(session, true)
}

// PartySearchRegist handles CSC_PARTYSEARCH_REGIST (2077).
func PartySearchRegist(session *network.Session, reader *network.Reader) {
	if reader.Size < partySearchRegistSize {
		log.Warningf("[PARTYSEARCH] invalid registration packet size: %d", reader.Size)
		sendPartySearchRegistResult(session, 3)
		return
	}

	maxMemberCount := reader.ReadByte()
	title := strings.TrimRight(string(reader.ReadBytes(partySearchTitleSize)), "\x00")
	currentContext, err := context.Parse(session)
	if err != nil {
		sendPartySearchRegistResult(session, 3)
		return
	}

	currentContext.Mutex.Lock()
	if currentContext.PartySearch.Registered {
		currentContext.Mutex.Unlock()
		sendPartySearchRegistResult(session, 1) // already registered
		return
	}
	if currentContext.Char == nil || currentContext.Char.Level < partySearchMinLevel {
		currentContext.Mutex.Unlock()
		sendPartySearchRegistResult(session, 7) // level limit
		return
	}
	party := clonePartyState(currentContext.Party)
	if party.ID != 0 && party.LeaderUserIdx != session.UserIdx {
		currentContext.Mutex.Unlock()
		sendPartySearchRegistResult(session, 4) // not party leader
		return
	}
	memberCount := len(party.Members)
	if memberCount == 0 {
		memberCount = 1
	}
	if maxMemberCount == 0 || int(maxMemberCount) > context.PartyMaxMembers {
		currentContext.Mutex.Unlock()
		sendPartySearchRegistResult(session, 2) // count over
		return
	}
	if int(maxMemberCount) < memberCount {
		currentContext.Mutex.Unlock()
		sendPartySearchRegistResult(session, 6) // current members do not fit
		return
	}
	currentContext.PartySearch = context.PartySearchState{
		Registered:     true,
		MaxMemberCount: maxMemberCount,
		Title:          title,
	}
	currentContext.Mutex.Unlock()

	sendPartySearchRegistResult(session, 0)
	log.Debugf("[PARTYSEARCH] registered: character=%d maxMembers=%d title=%q",
		currentContext.Char.Id, maxMemberCount, title)
}

// PartySearchList handles CSC_PARTYSEARCH_LIST (2081).
func PartySearchList(session *network.Session, reader *network.Reader) {
	if reader.Size < 10 {
		log.Warningf("[PARTYSEARCH] invalid packet size: %d", reader.Size)
		return
	}

	pkt := network.NewWriter(PARTY_SEARCH_LIST)
	entries := collectPartySearchEntries()
	pkt.WriteUint32(len(entries))
	for _, entry := range entries {
		pkt.WriteInt32(entry.LeaderCharacterID)
		pkt.WriteInt32(int32(entry.Level))
		pkt.WriteByte(entry.BattleStyle)
		pkt.WriteByte(entry.Channel)
		// WorldSvr copies cCharName here; it is a Pascal string.
		writePartyName(pkt, entry.Name)
		pkt.WriteByte(entry.MaxMemberCount)
		writeFixedPartyString(pkt, entry.Title, partySearchTitleSize)
		pkt.WriteByte(entry.CurrentMemberCount)
		pkt.WriteBytes(make([]byte, partySearchEntrySize-69)) // C++ struct tail padding
	}
	// S2C_PARTYSEARCH_LIST in WorldSvr contains a flexible-array placeholder
	// even when count is zero. Keep that trailing record for layout parity.
	pkt.WriteBytes(make([]byte, partySearchEntrySize))
	session.Send(pkt)
}

type partySearchEntry struct {
	LeaderCharacterID  int32
	Level              uint16
	BattleStyle        byte
	Channel            byte
	Name               string
	MaxMemberCount     byte
	Title              string
	CurrentMemberCount byte
}

func collectPartySearchEntries() []partySearchEntry {
	entries := make([]partySearchEntry, 0)
	for _, memberSession := range g_NetworkManager.Sessions() {
		ctx, err := context.Parse(memberSession)
		if err != nil {
			continue
		}
		ctx.Mutex.RLock()
		search := ctx.PartySearch
		party := clonePartyState(ctx.Party)
		char := ctx.Char
		if !search.Registered || char == nil || (party.ID != 0 && party.LeaderUserIdx != memberSession.UserIdx) {
			ctx.Mutex.RUnlock()
			continue
		}
		entry := partySearchEntry{
			LeaderCharacterID:  char.Id,
			Level:              char.Level,
			BattleStyle:        char.Style.BattleStyle,
			Channel:            byte(g_ServerSettings.ChannelId),
			Name:               char.Name,
			MaxMemberCount:     search.MaxMemberCount,
			Title:              search.Title,
			CurrentMemberCount: byte(len(party.Members)),
		}
		if entry.CurrentMemberCount == 0 {
			entry.CurrentMemberCount = 1
		}
		ctx.Mutex.RUnlock()
		entries = append(entries, entry)
	}
	return entries
}

func writeFixedPartyString(pkt *network.Writer, value string, size int) {
	data := make([]byte, size)
	copy(data, []byte(value))
	pkt.WriteBytes(data)
}

func makePartyState(inviterSession *network.Session, inviterContext *context.Context, targetSession *network.Session, targetContext *context.Context) (context.PartyState, byte) {
	inviterMember, inviterOK := partyMemberFromContext(inviterSession, inviterContext)
	targetMember, targetOK := partyMemberFromContext(targetSession, targetContext)
	if !inviterOK || !targetOK {
		return context.PartyState{}, partyInviteError
	}

	if inviterContext.Party.ID != 0 && targetContext.Party.ID != 0 {
		if inviterContext.Party.ID == targetContext.Party.ID {
			return context.PartyState{}, partyInviteAlreadyMember
		}
		return context.PartyState{}, partyInviteAlreadyMember
	}

	party := clonePartyState(inviterContext.Party)
	if party.ID == 0 {
		party = clonePartyState(targetContext.Party)
	}
	if party.ID == 0 {
		party.ID = atomic.AddInt32(&partySequence, 1)
		party.LeaderUserIdx = inviterMember.UserIdx
		party.LeaderCharacterID = inviterMember.CharacterID
		party.NormalLootingType = partyNormalLootingFree
		party.OwnerLootingType = partyOwnerLootingDice
	}

	if len(party.Members) >= context.PartyMaxMembers {
		return context.PartyState{}, partyInviteMaxMembers
	}
	if !partyHasMember(party, inviterMember.UserIdx) {
		party.Members = append(party.Members, inviterMember)
	}
	if !partyHasMember(party, targetMember.UserIdx) {
		if len(party.Members) >= context.PartyMaxMembers {
			return context.PartyState{}, partyInviteMaxMembers
		}
		party.Members = append(party.Members, targetMember)
	}
	return party, partyInviteOK
}

func findPartySession(characterID int32, name string) *network.Session {
	for _, session := range g_NetworkManager.Sessions() {
		ctx, err := context.Parse(session)
		if err != nil {
			continue
		}
		ctx.Mutex.RLock()
		currentID := int32(0)
		currentName := ""
		if ctx.Char != nil {
			currentID = ctx.Char.Id
			currentName = ctx.Char.Name
		}
		ctx.Mutex.RUnlock()
		if characterID > 0 && currentID == characterID {
			return session
		}
		if name != "" && currentName == name {
			return session
		}
	}

	// Some client paths reuse a typed OT_USER index in a character field.
	if characterID > 0 && uint32(characterID)>>24 == 0x01 {
		return g_NetworkManager.GetSession(uint16(uint32(characterID) & 0xFFFF))
	}
	return nil
}

func partyMemberFromContext(session *network.Session, ctx *context.Context) (context.PartyMember, bool) {
	if ctx.Char == nil {
		return context.PartyMember{}, false
	}
	return context.PartyMember{
		UserIdx:      session.UserIdx,
		CharacterID:  ctx.Char.Id,
		Level:        ctx.Char.Level,
		Channel:      byte(g_ServerSettings.ChannelId),
		BattleStyle:  ctx.Char.Style.BattleStyle,
		MemberStatus: context.PartyMemberStatusLogin,
		Name:         ctx.Char.Name,
	}, true
}

func clonePartyState(state context.PartyState) context.PartyState {
	state.Members = append([]context.PartyMember(nil), state.Members...)
	return state
}

func partyHasMember(state context.PartyState, userIdx uint16) bool {
	for _, member := range state.Members {
		if member.UserIdx == userIdx {
			return true
		}
	}
	return false
}

func partyMemberByCharacterID(state context.PartyState, characterID int32) (context.PartyMember, bool) {
	for _, member := range state.Members {
		if member.CharacterID == characterID {
			return member, true
		}
	}
	return context.PartyMember{}, false
}

func applyPartyState(state context.PartyState) []*network.Session {
	state = clonePartyState(state)
	sessions := make([]*network.Session, 0, len(state.Members))
	seen := make(map[uint16]bool, len(state.Members))
	for _, member := range state.Members {
		if seen[member.UserIdx] {
			continue
		}
		seen[member.UserIdx] = true
		session := g_NetworkManager.GetSession(member.UserIdx)
		if session == nil {
			continue
		}
		ctx, err := context.Parse(session)
		if err != nil {
			continue
		}
		ctx.Mutex.Lock()
		ctx.Party = clonePartyState(state)
		ctx.PartyInvite = context.PartyInviteState{}
		ctx.Mutex.Unlock()
		sessions = append(sessions, session)
	}
	return sessions
}

func clearPartyState(session *network.Session) {
	ctx, err := context.Parse(session)
	if err != nil {
		return
	}
	ctx.Mutex.Lock()
	ctx.Party = context.PartyState{}
	ctx.PartyInvite = context.PartyInviteState{}
	ctx.Mutex.Unlock()
}

func notifyPartyMemberStats(session *network.Session) {
	ctx, err := context.Parse(session)
	if err != nil {
		return
	}
	ctx.Mutex.RLock()
	party := clonePartyState(ctx.Party)
	ctx.Mutex.RUnlock()
	if party.ID == 0 || len(party.Members) < 2 {
		return
	}
	sendPartyMemberStatsToMembers(party)
}

func sendPartyMemberStatsToMembers(state context.PartyState) {
	state = clonePartyState(state)
	if state.ID == 0 || len(state.Members) < 2 {
		return
	}
	for _, member := range state.Members {
		session := g_NetworkManager.GetSession(member.UserIdx)
		if session == nil {
			continue
		}
		sendPartyMemberStats(session, state)
	}
}

type partyMemberRuntimeStats struct {
	member  context.PartyMember
	world   int32
	hp      uint32
	mp      uint32
	sp      uint32
	posX    int32
	posY    int32
	dungeon int32
}

func partyMemberRuntimeStatsFor(member context.PartyMember) partyMemberRuntimeStats {
	result := partyMemberRuntimeStats{member: member}
	session := g_NetworkManager.GetSession(member.UserIdx)
	if session == nil {
		result.member.MemberStatus = context.PartyMemberStatusLogoff
		return result
	}
	ctx, err := context.Parse(session)
	if err != nil {
		result.member.MemberStatus = context.PartyMemberStatusLogoff
		return result
	}

	ctx.Mutex.RLock()
	if ctx.Char != nil {
		result.member.CharacterID = ctx.Char.Id
		result.member.Level = ctx.Char.Level
		result.member.Channel = byte(g_ServerSettings.ChannelId)
		result.member.BattleStyle = ctx.Char.Style.BattleStyle
		result.member.MemberStatus = context.PartyMemberStatusLogin
		result.member.Name = ctx.Char.Name
		result.world = int32(ctx.Char.World)
		result.hp = uint32(ctx.Char.CurrentHP)<<16 | uint32(ctx.Char.MaxHP)
		result.mp = uint32(ctx.Char.CurrentMP)<<16 | uint32(ctx.Char.MaxMP)
		result.sp = uint32(ctx.Char.CurrentSP)<<16 | uint32(ctx.Char.MaxSP)
		result.posX = int32(ctx.Char.X)
		result.posY = int32(ctx.Char.Y)
	}
	ctx.Mutex.RUnlock()
	return result
}

func sendPartyMemberStats(session *network.Session, state context.PartyState) {
	pkt := network.NewWriter(NFY_PARTY_MEMBERS_STATS)
	pkt.WriteInt32(len(state.Members))

	for i := 0; i < context.PartyMaxMembers; i++ {
		if i >= len(state.Members) {
			pkt.WriteBytes(make([]byte, partyMemberTotalDataSize))
			continue
		}
		member := partyMemberRuntimeStatsFor(state.Members[i])
		pkt.WriteInt32(member.member.CharacterID)
		pkt.WriteInt32(int32(member.member.Level))
		pkt.WriteInt32(member.dungeon)
		pkt.WriteByte(member.member.Channel)
		pkt.WriteByte(member.member.BattleStyle)
		pkt.WriteInt32(int32(member.member.MemberStatus))
		writePartyName(pkt, member.member.Name)
		pkt.WriteInt32(0) // assistantIdx
		pkt.WriteInt32(member.world)
		pkt.WriteUint32(member.hp)
		pkt.WriteUint32(member.mp)
		pkt.WriteInt32(member.posX)
		pkt.WriteInt32(member.posY)
		pkt.WriteUint32(member.sp)
		pkt.WriteUint32(0) // raise spirit cooldown in milliseconds
		pkt.WriteInt32(0)  // useBBeadPlus: BOOL
		pkt.WriteUint32(0) // assistantExpiredReminSec
	}
	session.Send(pkt)
}

func lockPartyContexts(firstSession *network.Session, firstContext *context.Context, secondSession *network.Session, secondContext *context.Context) {
	if firstSession.UserIdx < secondSession.UserIdx {
		firstContext.Mutex.Lock()
		secondContext.Mutex.Lock()
	} else {
		secondContext.Mutex.Lock()
		firstContext.Mutex.Lock()
	}
}

func unlockPartyContexts(firstSession *network.Session, firstContext *context.Context, secondSession *network.Session, secondContext *context.Context) {
	if firstSession.UserIdx < secondSession.UserIdx {
		secondContext.Mutex.Unlock()
		firstContext.Mutex.Unlock()
	} else {
		firstContext.Mutex.Unlock()
		secondContext.Mutex.Unlock()
	}
}

func sendPartyInviteResponse(session *network.Session, result byte, invited context.PartyMember) {
	pkt := network.NewWriter(PARTY_INVITE)
	pkt.WriteByte(result)
	pkt.WriteInt32(invited.CharacterID)
	pkt.WriteByte(invited.Channel)
	pkt.WriteByte(invited.BattleStyle)
	pkt.WriteInt32(int32(invited.Level))
	writePartyName(pkt, invited.Name)
	session.Send(pkt)
}

func sendPartyInviteNotification(session *network.Session, inviter context.PartyMember) {
	pkt := network.NewWriter(NFY_PARTY_INVITE)
	pkt.WriteInt32(inviter.CharacterID)
	pkt.WriteByte(inviter.Channel)
	pkt.WriteByte(inviter.BattleStyle)
	pkt.WriteInt32(int32(inviter.Level))
	writePartyName(pkt, inviter.Name)
	session.Send(pkt)
}

func sendPartyInviteAnswer(session *network.Session, result byte) {
	pkt := network.NewWriter(PARTY_INVITE_RESULT)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func sendPartyInviteResult(session *network.Session, accepted bool) {
	pkt := network.NewWriter(NFY_PARTY_INVITE_RESULT)
	if accepted {
		pkt.WriteInt32(1)
	} else {
		pkt.WriteInt32(0)
	}
	session.Send(pkt)
}

func sendPartyInviteCancelResult(session *network.Session, success bool) {
	pkt := network.NewWriter(PARTY_INVITE_CANCEL)
	if success {
		pkt.WriteInt32(1)
	} else {
		pkt.WriteInt32(0)
	}
	session.Send(pkt)
}

func sendPartyInviteCancelNotification(session *network.Session) {
	session.Send(network.NewWriter(NFY_PARTY_INVITE_CANCEL))
}

func sendPartyLeaveResult(session *network.Session, success bool) {
	pkt := network.NewWriter(PARTY_LEAVE)
	if success {
		pkt.WriteInt32(1)
	} else {
		pkt.WriteInt32(0)
	}
	session.Send(pkt)
}

func sendPartyKickoutResult(session *network.Session, success bool) {
	pkt := network.NewWriter(PARTY_KICKOUT)
	if success {
		pkt.WriteInt32(1) // BOOL
	} else {
		pkt.WriteInt32(0) // BOOL
	}
	session.Send(pkt)
}

func sendPartyKickoutNotification(session *network.Session, characterID int32) {
	pkt := network.NewWriter(NFY_PARTY_MEMBER_KICKOUT)
	pkt.WriteInt32(characterID)
	session.Send(pkt)
}

func sendPartyLeaderChangeResult(session *network.Session, success bool) {
	pkt := network.NewWriter(PARTY_LEADER_CHANGE)
	if success {
		pkt.WriteInt32(1) // BOOL
	} else {
		pkt.WriteInt32(0) // BOOL
	}
	session.Send(pkt)
}

func sendPartyLeaderChangeNotification(session *network.Session, characterID int32) {
	pkt := network.NewWriter(NFY_PARTY_LEADER_CHANGE)
	pkt.WriteInt32(characterID)
	session.Send(pkt)
}

func sendPartyAuthChangeResult(session *network.Session, success bool) {
	pkt := network.NewWriter(PARTY_AUTH_CHANGE)
	if success {
		pkt.WriteInt32(1) // BOOL
	} else {
		pkt.WriteInt32(0) // BOOL
	}
	session.Send(pkt)
}

func sendPartyAuthChangeNotification(session *network.Session, leaderOnly bool) {
	pkt := network.NewWriter(NFY_PARTY_AUTH_CHANGE)
	if leaderOnly {
		pkt.WriteInt32(1) // BOOL
	} else {
		pkt.WriteInt32(0) // BOOL
	}
	session.Send(pkt)
}

func sendPartyLootingTypeResult(session *network.Session, success bool) {
	pkt := network.NewWriter(PARTY_LOOTING_TYPE)
	if success {
		pkt.WriteInt32(1) // BOOL
	} else {
		pkt.WriteInt32(0) // BOOL
	}
	session.Send(pkt)
}

func sendPartyLootingTypeNotification(session *network.Session, normal byte, owner byte) {
	pkt := network.NewWriter(NFY_PARTY_LOOTING_TYPE)
	pkt.WriteInt32(int32(normal))
	pkt.WriteInt32(int32(owner))
	session.Send(pkt)
}

func sendPartySearchRegistResult(session *network.Session, result byte) {
	pkt := network.NewWriter(PARTY_SEARCH_REGIST)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func sendPartyMemberOut(session *network.Session, characterID int32) {
	pkt := network.NewWriter(NFY_PARTY_MEMBER_OUT)
	pkt.WriteInt32(characterID)
	session.Send(pkt)
}

func sendPartyStats(session *network.Session, state context.PartyState) {
	pkt := network.NewWriter(NFY_PARTY_STATS)
	writePartyStatsPayload(pkt, state)
	session.Send(pkt)
}

func sendPartyStatsReconnect(session *network.Session) {
	pkt := network.NewWriter(NFY_PARTY_STATS_RECONNECT)
	pkt.WriteInt32(0) // hasParty: BOOL
	pkt.WriteInt32(0) // dungeon index
	writePartyStatsPayload(pkt, context.PartyState{})
	session.Send(pkt)
	log.Debugf("[PARTYINIT] initialized party state for user=%d", session.UserIdx)
}

func writePartyStatsPayload(pkt *network.Writer, state context.PartyState) {
	pkt.WriteInt32(state.ID)
	pkt.WriteInt32(state.LeaderCharacterID)
	pkt.WriteBool(state.LeaderOnlyInviteAuthority)
	pkt.WriteInt32(0) // party-search registration
	normalLooting := state.NormalLootingType
	if normalLooting == 0 {
		normalLooting = partyNormalLootingFree
	}
	ownerLooting := state.OwnerLootingType
	if ownerLooting == 0 {
		ownerLooting = partyOwnerLootingDice
	}
	pkt.WriteByte(normalLooting)
	pkt.WriteByte(ownerLooting)
	pkt.WriteInt32(len(state.Members))

	for i := 0; i < context.PartyMaxMembers; i++ {
		if i >= len(state.Members) {
			pkt.WriteBytes(make([]byte, partyMemberDefaultDataSize))
			continue
		}
		member := state.Members[i]
		pkt.WriteInt32(member.CharacterID)
		pkt.WriteInt32(int32(member.Level))
		pkt.WriteInt32(0) // dungeon index
		pkt.WriteByte(member.Channel)
		pkt.WriteByte(member.BattleStyle)
		pkt.WriteInt32(int32(member.MemberStatus))
		writePartyName(pkt, member.Name)
		pkt.WriteInt32(0) // assistantIdx
	}
}

func writePartyName(pkt *network.Writer, name string) {
	data := make([]byte, partyMaxNameLength)
	nameBytes := []byte(name)
	if len(nameBytes) > partyMaxNameLength-1 {
		nameBytes = nameBytes[:partyMaxNameLength-1]
	}
	data[0] = byte(len(nameBytes) + 1)
	copy(data[1:], nameBytes)
	pkt.WriteBytes(data)
}

func decodePartyName(raw string) string {
	data := []byte(raw)
	if len(data) > 0 {
		length := int(data[0])
		if length > 0 && length <= len(data)-1 {
			return string(data[1 : 1+length-1])
		}
	}
	return strings.TrimRight(raw, "\x00")
}
