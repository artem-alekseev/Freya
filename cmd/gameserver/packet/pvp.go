package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/network"
)

const (
	pvpRequestPacketSize  = 19 // header + target user index + target character index + type
	pvpResponsePacketSize = 20 // header + user index + character index + type + result
	pvpPendingPacketSize  = 12 // header + type + ending result
	pvpCancelPacketSize   = 19 // header + target user index + target character index + type
	pvpTypePerson         = 2
	pvpStateNone          = 0
	pvpStateWaiting       = 1
	pvpResultComplete     = 0
	pvpResultFail         = 1
	pvpResultIgnored      = 2
	pvpResultBusy         = 3
)

// PVPRequest handles CSC_PVPREQUEST (330). The current client sends the
// personal-duel form: target user index, target character index and type.
// WorldSvr replies to the requester and then notifies the target with 331.
func PVPRequest(session *network.Session, reader *network.Reader) {
	if reader.Size < pvpRequestPacketSize {
		log.Warningf("[PVPREQUEST] invalid packet size: %d", reader.Size)
		sendPVPRequestResult(session, pvpResultFail)
		return
	}

	targetUserIndex := decodeUserObjectIndex(reader.ReadInt32())
	targetCharacterID := reader.ReadInt32()
	pvpType := reader.ReadByte()
	if pvpType != pvpTypePerson {
		log.Warningf("[PVPREQUEST] unsupported PvP type: user=%d char=%d type=%d",
			targetUserIndex, targetCharacterID, pvpType)
		sendPVPRequestResult(session, pvpResultIgnored)
		return
	}

	senderContext, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendPVPRequestResult(session, pvpResultFail)
		return
	}

	senderContext.Mutex.RLock()
	if senderContext.Char == nil {
		senderContext.Mutex.RUnlock()
		sendPVPRequestResult(session, pvpResultFail)
		return
	}
	senderCharacterID := senderContext.Char.Id
	senderContext.Mutex.RUnlock()

	if targetUserIndex < 0 || targetUserIndex > int32(^uint16(0)) ||
		targetCharacterID <= 0 || targetUserIndex == int32(session.UserIdx) {
		log.Warningf("[PVPREQUEST] invalid target: sender=%d targetUser=%d targetChar=%d",
			senderCharacterID, targetUserIndex, targetCharacterID)
		sendPVPRequestResult(session, pvpResultFail)
		return
	}

	targetSession := g_NetworkManager.GetSession(uint16(targetUserIndex))
	if targetSession == nil {
		log.Warningf("[PVPREQUEST] target session not found: sender=%d targetUser=%d",
			senderCharacterID, targetUserIndex)
		sendPVPRequestResult(session, pvpResultFail)
		return
	}

	targetContext, err := context.Parse(targetSession)
	if err != nil {
		log.Warningf("[PVPREQUEST] target is not initialized: sender=%d targetUser=%d",
			senderCharacterID, targetUserIndex)
		sendPVPRequestResult(session, pvpResultFail)
		return
	}

	targetContext.Mutex.RLock()
	actualTargetCharacterID := int32(0)
	if targetContext.Char != nil {
		actualTargetCharacterID = targetContext.Char.Id
	}
	if actualTargetCharacterID != targetCharacterID {
		targetContext.Mutex.RUnlock()
		log.Warningf("[PVPREQUEST] target character mismatch: sender=%d targetUser=%d got=%d want=%d",
			senderCharacterID, targetUserIndex, actualTargetCharacterID, targetCharacterID)
		sendPVPRequestResult(session, pvpResultFail)
		return
	}
	targetContext.Mutex.RUnlock()

	if !reservePVPWaitingState(session, targetSession, senderCharacterID, targetCharacterID) {
		log.Warningf("[PVPREQUEST] one of the players is already in PvP: sender=%d targetUser=%d",
			senderCharacterID, targetUserIndex)
		sendPVPRequestResult(session, pvpResultBusy)
		return
	}

	sendPVPRequestResult(session, pvpResultComplete)

	notification := network.NewWriter(NFY_PVP_REQUEST)
	notification.WriteInt32(senderCharacterID)
	notification.WriteByte(pvpType)
	targetSession.Send(notification)

	log.Debugf("[PVPREQUEST] sent: sender=%d targetUser=%d targetChar=%d type=%d",
		senderCharacterID, targetUserIndex, targetCharacterID, pvpType)
}

func sendPVPRequestResult(session *network.Session, result byte) {
	pkt := network.NewWriter(PVP_REQUEST)
	pkt.WriteByte(result)
	session.Send(pkt)
}

// PVPResponse handles C2S_PVPRESPONSE (332) for personal PvP.
func PVPResponse(session *network.Session, reader *network.Reader) {
	if reader.Size < pvpResponsePacketSize {
		log.Warningf("[PVPRESPONSE] invalid packet size: %d", reader.Size)
		sendPVPResponseResult(session, pvpResultFail)
		return
	}

	requesterUserIndex := decodeUserObjectIndex(reader.ReadInt32())
	requesterCharacterID := reader.ReadInt32()
	pvpType := reader.ReadByte()
	requestResult := reader.ReadByte()
	if pvpType != pvpTypePerson || (requestResult != 0 && requestResult != 1) {
		log.Warningf("[PVPRESPONSE] unsupported response: user=%d char=%d type=%d result=%d",
			requesterUserIndex, requesterCharacterID, pvpType, requestResult)
		sendPVPResponseResult(session, pvpResultIgnored)
		return
	}

	responderContext, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendPVPResponseResult(session, pvpResultFail)
		return
	}
	if requesterUserIndex < 0 || requesterUserIndex > int32(^uint16(0)) {
		log.Warningf("[PVPRESPONSE] invalid requester user index: %d", requesterUserIndex)
		sendPVPResponseResult(session, pvpResultFail)
		return
	}

	requesterSession := g_NetworkManager.GetSession(uint16(requesterUserIndex))
	if requesterSession == nil {
		log.Warningf("[PVPRESPONSE] requester session not found: user=%d", requesterUserIndex)
		sendPVPResponseResult(session, pvpResultFail)
		return
	}
	if requesterSession.UserIdx == session.UserIdx {
		log.Warningf("[PVPRESPONSE] requester and responder are the same user: %d", requesterUserIndex)
		sendPVPResponseResult(session, pvpResultFail)
		return
	}
	requesterContext, err := context.Parse(requesterSession)
	if err != nil {
		log.Warningf("[PVPRESPONSE] requester is not initialized: user=%d", requesterUserIndex)
		sendPVPResponseResult(session, pvpResultFail)
		return
	}

	responderUserIndex := session.UserIdx
	if responderUserIndex < requesterSession.UserIdx {
		responderContext.Mutex.Lock()
		requesterContext.Mutex.Lock()
	} else {
		requesterContext.Mutex.Lock()
		responderContext.Mutex.Lock()
	}
	unlockPVPContexts := func() {
		if responderUserIndex < requesterSession.UserIdx {
			requesterContext.Mutex.Unlock()
			responderContext.Mutex.Unlock()
			return
		}
		responderContext.Mutex.Unlock()
		requesterContext.Mutex.Unlock()
	}
	valid := responderContext.PVP.Type == pvpStateWaiting &&
		responderContext.PVP.OpponentUserIdx == requesterSession.UserIdx &&
		responderContext.PVP.OpponentCharacterID == requesterCharacterID &&
		requesterContext.PVP.Type == pvpStateWaiting &&
		requesterContext.PVP.OpponentUserIdx == responderUserIndex
	if valid && responderContext.Char != nil && requesterContext.Char != nil {
		valid = requesterContext.Char.Id == requesterCharacterID
	}
	if !valid {
		unlockPVPContexts()
		log.Warningf("[PVPRESPONSE] no matching pending request: responderUser=%d requesterUser=%d requesterChar=%d",
			responderUserIndex, requesterUserIndex, requesterCharacterID)
		sendPVPResponseResult(session, pvpResultFail)
		return
	}

	responderCharacterID := responderContext.Char.Id
	responderHP := responderContext.Char.CurrentHP
	responderMaxHP := responderContext.Char.MaxHP
	requesterHP := requesterContext.Char.CurrentHP
	requesterMaxHP := requesterContext.Char.MaxHP
	if requestResult == 0 {
		responderContext.PVP.Type = pvpType
		requesterContext.PVP.Type = pvpType
	} else {
		responderContext.PVP = context.PVPState{}
		requesterContext.PVP = context.PVPState{}
	}
	unlockPVPContexts()

	sendPVPResponseResult(session, pvpResultComplete)

	notification := network.NewWriter(NFY_PVP_RESPONSE)
	notification.WriteInt32(responderCharacterID)
	notification.WriteByte(pvpType)
	notification.WriteByte(requestResult)
	requesterSession.Send(notification)

	if requestResult == 0 {
		// WorldSvr sends the current PvP HP snapshot after the duel becomes
		// active. The client uses it to finish linking the opponent as a PvP
		// enemy, not only to draw the HP bar.
		sendPVPCharacterInfo(requesterSession, responderCharacterID, responderHP, responderMaxHP)
		sendPVPCharacterInfo(session, requesterCharacterID, requesterHP, requesterMaxHP)
	}

	log.Debugf("[PVPRESPONSE] processed: responder=%d requester=%d type=%d result=%d",
		responderCharacterID, requesterCharacterID, pvpType, requestResult)
}

func sendPVPResponseResult(session *network.Session, result byte) {
	pkt := network.NewWriter(PVP_RESPONSE)
	pkt.WriteByte(result)
	session.Send(pkt)
}

// PVPPending handles CSC_PVPENDING (334), which ends an active personal PvP.
func PVPPending(session *network.Session, reader *network.Reader) {
	if reader.Size < pvpPendingPacketSize {
		log.Warningf("[PVPENDING] invalid packet size: %d", reader.Size)
		sendPVPPendingResult(session, pvpResultFail)
		return
	}

	pvpType := reader.ReadByte()
	endingResult := reader.ReadByte()
	if pvpType != pvpTypePerson || endingResult > 3 {
		log.Warningf("[PVPENDING] invalid request: type=%d result=%d", pvpType, endingResult)
		sendPVPPendingResult(session, pvpResultFail)
		return
	}

	senderContext, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendPVPPendingResult(session, pvpResultFail)
		return
	}

	senderContext.Mutex.RLock()
	targetUserIndex := senderContext.PVP.OpponentUserIdx
	senderContext.Mutex.RUnlock()
	targetSession := g_NetworkManager.GetSession(targetUserIndex)
	if targetSession == nil || targetSession.UserIdx == session.UserIdx {
		log.Warningf("[PVPENDING] target session not found: user=%d", targetUserIndex)
		sendPVPPendingResult(session, pvpResultFail)
		return
	}

	targetContext, err := context.Parse(targetSession)
	if err != nil {
		log.Warningf("[PVPENDING] target is not initialized: user=%d", targetUserIndex)
		sendPVPPendingResult(session, pvpResultFail)
		return
	}

	if session.UserIdx < targetSession.UserIdx {
		senderContext.Mutex.Lock()
		targetContext.Mutex.Lock()
	} else {
		targetContext.Mutex.Lock()
		senderContext.Mutex.Lock()
	}

	valid := senderContext.PVP.Type == pvpTypePerson &&
		senderContext.PVP.OpponentUserIdx == targetSession.UserIdx &&
		targetContext.PVP.Type == pvpTypePerson &&
		targetContext.PVP.OpponentUserIdx == session.UserIdx
	if !valid {
		if session.UserIdx < targetSession.UserIdx {
			targetContext.Mutex.Unlock()
			senderContext.Mutex.Unlock()
		} else {
			senderContext.Mutex.Unlock()
			targetContext.Mutex.Unlock()
		}
		log.Warningf("[PVPENDING] no matching active PvP: user=%d targetUser=%d",
			session.UserIdx, targetSession.UserIdx)
		sendPVPPendingResult(session, pvpResultFail)
		return
	}

	senderContext.PVP = context.PVPState{}
	targetContext.PVP = context.PVPState{}
	if session.UserIdx < targetSession.UserIdx {
		targetContext.Mutex.Unlock()
		senderContext.Mutex.Unlock()
	} else {
		senderContext.Mutex.Unlock()
		targetContext.Mutex.Unlock()
	}

	sendPVPPendingResult(session, pvpResultComplete)
	notification := network.NewWriter(NFY_PVP_PENDING)
	notification.WriteByte(pvpType)
	notification.WriteByte(endingResult)
	targetSession.Send(notification)

	log.Debugf("[PVPENDING] processed: user=%d targetUser=%d type=%d result=%d",
		session.UserIdx, targetSession.UserIdx, pvpType, endingResult)
}

func sendPVPPendingResult(session *network.Session, result byte) {
	pkt := network.NewWriter(PVP_PENDING)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func sendPVPCharacterInfo(session *network.Session, characterID int32, hp, maxHP uint16) {
	pkt := network.NewWriter(NFY_PVP_CHAR_INFO)
	pkt.WriteInt32(characterID)
	pkt.WriteUint16(hp)
	pkt.WriteUint16(maxHP)
	session.Send(pkt)
}

// PVPCancel handles C2S_PVPCANCEL (336) for personal PvP.
func PVPCancel(session *network.Session, reader *network.Reader) {
	if reader.Size < pvpCancelPacketSize {
		log.Warningf("[PVPCANCEL] invalid packet size: %d", reader.Size)
		sendPVPCancelResult(session, pvpResultFail)
		return
	}

	targetUserIndex := decodeUserObjectIndex(reader.ReadInt32())
	targetCharacterID := reader.ReadInt32()
	pvpType := reader.ReadByte()
	if pvpType != pvpTypePerson || targetUserIndex < 0 || targetUserIndex > int32(^uint16(0)) ||
		targetCharacterID <= 0 || targetUserIndex == int32(session.UserIdx) {
		log.Warningf("[PVPCANCEL] invalid request: user=%d char=%d type=%d",
			targetUserIndex, targetCharacterID, pvpType)
		sendPVPCancelResult(session, pvpResultFail)
		return
	}

	senderContext, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendPVPCancelResult(session, pvpResultFail)
		return
	}
	targetSession := g_NetworkManager.GetSession(uint16(targetUserIndex))
	if targetSession == nil {
		log.Warningf("[PVPCANCEL] target session not found: user=%d", targetUserIndex)
		sendPVPCancelResult(session, pvpResultFail)
		return
	}
	targetContext, err := context.Parse(targetSession)
	if err != nil {
		log.Warningf("[PVPCANCEL] target is not initialized: user=%d", targetUserIndex)
		sendPVPCancelResult(session, pvpResultFail)
		return
	}

	if session.UserIdx < targetSession.UserIdx {
		senderContext.Mutex.Lock()
		targetContext.Mutex.Lock()
	} else {
		targetContext.Mutex.Lock()
		senderContext.Mutex.Lock()
	}
	unlockPVPContexts := func() {
		if session.UserIdx < targetSession.UserIdx {
			targetContext.Mutex.Unlock()
			senderContext.Mutex.Unlock()
			return
		}
		senderContext.Mutex.Unlock()
		targetContext.Mutex.Unlock()
	}

	valid := senderContext.Char != nil && targetContext.Char != nil &&
		senderContext.PVP.OpponentUserIdx == targetSession.UserIdx &&
		senderContext.PVP.OpponentCharacterID == targetCharacterID &&
		targetContext.PVP.OpponentUserIdx == session.UserIdx
	if !valid {
		unlockPVPContexts()
		log.Warningf("[PVPCANCEL] no matching PvP state: user=%d targetUser=%d targetChar=%d",
			session.UserIdx, targetUserIndex, targetCharacterID)
		sendPVPCancelResult(session, pvpResultFail)
		return
	}

	senderContext.PVP = context.PVPState{}
	targetContext.PVP = context.PVPState{}
	unlockPVPContexts()

	sendPVPCancelResult(session, pvpResultComplete)
	notification := network.NewWriter(NFY_PVP_CANCEL)
	notification.WriteByte(pvpType)
	targetSession.Send(notification)

	log.Debugf("[PVPCANCEL] processed: user=%d targetUser=%d targetChar=%d type=%d",
		session.UserIdx, targetUserIndex, targetCharacterID, pvpType)
}

func sendPVPCancelResult(session *network.Session, result byte) {
	pkt := network.NewWriter(PVP_CANCEL)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func reservePVPWaitingState(sender, target *network.Session, senderCharacterID, targetCharacterID int32) bool {
	senderContext, err := context.Parse(sender)
	if err != nil {
		return false
	}
	targetContext, err := context.Parse(target)
	if err != nil {
		return false
	}

	if sender.UserIdx < target.UserIdx {
		senderContext.Mutex.Lock()
		targetContext.Mutex.Lock()
	} else {
		targetContext.Mutex.Lock()
		senderContext.Mutex.Lock()
	}
	defer senderContext.Mutex.Unlock()
	defer targetContext.Mutex.Unlock()

	if senderContext.PVP.Type != pvpStateNone || targetContext.PVP.Type != pvpStateNone {
		return false
	}

	senderContext.PVP = context.PVPState{
		Type:                pvpStateWaiting,
		OpponentUserIdx:     target.UserIdx,
		OpponentCharacterID: targetCharacterID,
	}
	targetContext.PVP = context.PVPState{
		Type:                pvpStateWaiting,
		OpponentUserIdx:     sender.UserIdx,
		OpponentCharacterID: senderCharacterID,
	}
	return true
}
