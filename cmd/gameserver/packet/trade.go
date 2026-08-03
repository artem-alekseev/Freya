package packet

import (
	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
)

const (
	tradeRequestPacketSize    = 18 // header + target character index + target object index
	tradeOpenPacketSize       = 11 // header + trade event
	tradeClosePacketSize      = 11 // header + trade event
	tradeAddItemPacketSize    = 14 // header + source inventory slot + trade slot
	tradeAddAlzPacketSize     = 18 // header + TALZ
	tradeRemoveItemPacketSize = 14 // header + trade slot + inventory slot
	tradeRequestListMinSize   = 11 // header + item count
	tradeMaxSlot              = 255

	tradeResultInvalid       byte   = 0x00
	tradeResultProgress      byte   = 0x1E
	tradeResultDenial        byte   = 0x21
	tradeResultRequest       byte   = 0x22
	tradeResultReady         byte   = 0x23
	tradeResultRequestCancel byte   = 0x29
	tradeResultAccept        byte   = 0x2C
	tradeResultReject        byte   = 0x2D
	tradeResultAutoReject    byte   = 0x2E
	tradeResultSubmitCancel  byte   = 0x2A
	tradeResultTradeCancel   byte   = 0x2B
	tradeResultSubmit        byte   = 0x2F
	tradeResultRequestList   byte   = 0x25
	tradeResultCompleted     byte   = 0x26
	tradeResultAddSuccess    byte   = 0x27
	tradeResultRemoveSuccess byte   = 0x28
	tradeResultResets        byte   = 0x1F
	tradeAlzItemKind         uint32 = 13
)

// TradeRequest handles REQ_TRADEREQUST (260).
// The client sends the target character id followed by the full OT_USER index.
func TradeRequest(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeRequestPacketSize {
		log.Warningf("[TRADEREQUEST] invalid packet size: %d", reader.Size)
		sendTradeEvent(session, 0, tradeResultDenial)
		return
	}

	targetCharacterID := reader.ReadInt32()
	targetUserIndex := decodeUserObjectIndex(reader.ReadInt32())

	senderContext, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendTradeEvent(session, 0, tradeResultDenial)
		return
	}

	senderContext.Mutex.RLock()
	senderCharacterID := int32(0)
	if senderContext.Char != nil {
		senderCharacterID = senderContext.Char.Id
	}
	senderContext.Mutex.RUnlock()

	if senderCharacterID <= 0 || targetUserIndex < 0 ||
		targetUserIndex > int32(^uint16(0)) || targetUserIndex == int32(session.UserIdx) {
		log.Warningf("[TRADEREQUEST] invalid target: sender=%d targetUser=%d targetChar=%d",
			senderCharacterID, targetUserIndex, targetCharacterID)
		sendTradeEvent(session, senderCharacterID, tradeResultDenial)
		return
	}

	targetSession := g_NetworkManager.GetSession(uint16(targetUserIndex))
	if targetSession == nil {
		log.Warningf("[TRADEREQUEST] target session not found: sender=%d targetUser=%d",
			senderCharacterID, targetUserIndex)
		sendTradeEvent(session, senderCharacterID, tradeResultDenial)
		return
	}

	targetContext, err := context.Parse(targetSession)
	if err != nil {
		log.Warningf("[TRADEREQUEST] target is not initialized: user=%d", targetUserIndex)
		sendTradeEvent(session, senderCharacterID, tradeResultDenial)
		return
	}

	targetContext.Mutex.RLock()
	actualTargetCharacterID := int32(0)
	if targetContext.Char != nil {
		actualTargetCharacterID = targetContext.Char.Id
	}
	targetContext.Mutex.RUnlock()

	if actualTargetCharacterID != targetCharacterID {
		log.Warningf("[TRADEREQUEST] target character mismatch: user=%d got=%d want=%d",
			targetUserIndex, actualTargetCharacterID, targetCharacterID)
		sendTradeEvent(session, senderCharacterID, tradeResultDenial)
		return
	}

	if !reserveTradeRequest(session, targetSession, targetCharacterID, senderCharacterID) {
		log.Warningf("[TRADEREQUEST] one of the players is already trading: sender=%d target=%d",
			senderCharacterID, targetCharacterID)
		sendTradeEvent(session, senderCharacterID, tradeResultProgress)
		return
	}

	// The target receives the request notification. The requester receives
	// TR_TRDRDY only after the target accepts it.
	sendTradeEvent(targetSession, senderCharacterID, tradeResultRequest)
	log.Debugf("[TRADEREQUEST] sent: sender=%d targetUser=%d targetChar=%d",
		senderCharacterID, targetUserIndex, targetCharacterID)
}

// TradeOpenEvent handles CSC_TRADEOPNEVT (261): accept, reject or cancel.
func TradeOpenEvent(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeOpenPacketSize {
		log.Warningf("[TRADEOPNEVT] invalid packet size: %d", reader.Size)
		sendTradeOpenResult(session, tradeResultInvalid)
		return
	}

	event := reader.ReadByte()
	if event != tradeResultRequestCancel && event != tradeResultAccept &&
		event != tradeResultReject && event != tradeResultAutoReject {
		log.Warningf("[TRADEOPNEVT] unsupported event: %d", event)
		sendTradeOpenResult(session, tradeResultInvalid)
		return
	}

	senderContext, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		sendTradeOpenResult(session, tradeResultInvalid)
		return
	}

	senderContext.Mutex.RLock()
	tradeState := senderContext.Trade
	senderContext.Mutex.RUnlock()

	targetSession := g_NetworkManager.GetSession(tradeState.OpponentUserIdx)
	if targetSession == nil || targetSession.UserIdx == session.UserIdx {
		log.Warningf("[TRADEOPNEVT] target session not found: user=%d", tradeState.OpponentUserIdx)
		sendTradeOpenResult(session, tradeResultInvalid)
		return
	}

	targetContext, err := context.Parse(targetSession)
	if err != nil {
		log.Warningf("[TRADEOPNEVT] target is not initialized: user=%d", targetSession.UserIdx)
		sendTradeOpenResult(session, tradeResultInvalid)
		return
	}

	if session.UserIdx < targetSession.UserIdx {
		senderContext.Mutex.Lock()
		targetContext.Mutex.Lock()
	} else {
		targetContext.Mutex.Lock()
		senderContext.Mutex.Lock()
	}

	valid := tradeState.Status == context.TradeStatePending &&
		targetContext.Trade.Status == context.TradeStatePending &&
		targetContext.Trade.OpponentUserIdx == session.UserIdx &&
		targetContext.Trade.OpponentCharacterID == senderContext.Char.Id
	if event == tradeResultRequestCancel {
		valid = valid && tradeState.Role == context.TradeRoleRequester
	} else {
		valid = valid && tradeState.Role == context.TradeRoleTarget
	}

	if !valid {
		if session.UserIdx < targetSession.UserIdx {
			targetContext.Mutex.Unlock()
			senderContext.Mutex.Unlock()
		} else {
			senderContext.Mutex.Unlock()
			targetContext.Mutex.Unlock()
		}
		log.Warningf("[TRADEOPNEVT] no matching pending request: user=%d event=%d",
			session.UserIdx, event)
		sendTradeOpenResult(session, tradeResultInvalid)
		return
	}

	requesterSession := session
	requesterContext := senderContext
	targetContextForNotification := targetContext
	if tradeState.Role == context.TradeRoleTarget {
		requesterSession = targetSession
		requesterContext = targetContext
		targetContextForNotification = senderContext
	}

	requesterCharacterID := requesterContext.Char.Id
	targetCharacterID := targetContextForNotification.Char.Id
	if event == tradeResultAccept {
		senderContext.Trade.Status = context.TradeStateOpen
		targetContext.Trade.Status = context.TradeStateOpen
	} else {
		senderContext.Trade = context.TradeState{}
		targetContext.Trade = context.TradeState{}
	}

	if session.UserIdx < targetSession.UserIdx {
		targetContext.Mutex.Unlock()
		senderContext.Mutex.Unlock()
	} else {
		senderContext.Mutex.Unlock()
		targetContext.Mutex.Unlock()
	}

	if event == tradeResultAccept {
		// The accepting client gets the local open response; the requester gets
		// the notification that both sides are ready to enter the trade UI.
		sendTradeOpenResult(session, tradeResultAccept)
		sendTradeEvent(requesterSession, targetCharacterID, tradeResultReady)
		log.Debugf("[TRADEOPNEVT] accepted: requester=%d target=%d",
			requesterCharacterID, targetCharacterID)
		return
	}

	sendTradeOpenResult(session, event)
	// A requester cancels its own waiting dialog, so the cancellation
	// notification must go to the target. For reject/auto-reject the target
	// is the current session and the requester receives the notification.
	notificationSession := requesterSession
	if tradeState.Role == context.TradeRoleRequester {
		notificationSession = targetSession
	}
	sendTradeEvent(notificationSession, targetCharacterID, event)
	log.Debugf("[TRADEOPNEVT] closed: requester=%d target=%d event=%d",
		requesterCharacterID, targetCharacterID, event)
}

// TradeCloseEvent handles CSC_TRADECLSEVT (262). It is used for submitting,
// cancelling the submit state, and closing an already opened trade window.
func TradeCloseEvent(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeClosePacketSize {
		log.Warningf("[TRADECLSEVT] invalid packet size: %d", reader.Size)
		sendTradePacketResult(session, TRADE_CLOSE_EVENT, tradeResultInvalid)
		return
	}

	event := reader.ReadByte()
	if event != tradeResultSubmitCancel && event != tradeResultTradeCancel && event != tradeResultSubmit {
		log.Warningf("[TRADECLSEVT] unsupported event: %d", event)
		sendTradePacketResult(session, TRADE_CLOSE_EVENT, tradeResultInvalid)
		return
	}

	currentSession, currentContext, peerSession, peerContext, ok := tradePair(session)
	if !ok {
		sendTradePacketResult(session, TRADE_CLOSE_EVENT, tradeResultInvalid)
		return
	}

	lockTradeContexts(currentSession, currentContext, peerSession, peerContext)
	if currentContext.Trade.Status != context.TradeStateOpen ||
		peerContext.Trade.Status != context.TradeStateOpen {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		sendTradePacketResult(session, TRADE_CLOSE_EVENT, tradeResultInvalid)
		return
	}

	currentCharacterID := currentContext.Char.Id
	peerSubmitted := peerContext.Trade.Submitted
	switch event {
	case tradeResultSubmit:
		if currentContext.Trade.Submitted {
			unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
			sendTradePacketResult(session, TRADE_CLOSE_EVENT, tradeResultInvalid)
			return
		}
		currentContext.Trade.Submitted = true
	case tradeResultSubmitCancel:
		currentContext.Trade.Submitted = false
	case tradeResultTradeCancel:
		currentContext.Trade = context.TradeState{}
		peerContext.Trade = context.TradeState{}
	}
	bothSubmitted := event == tradeResultSubmit && peerSubmitted
	unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)

	sendTradePacketResult(session, TRADE_CLOSE_EVENT, event)
	if event == tradeResultTradeCancel {
		sendTradeEvent(peerSession, currentCharacterID, event)
		log.Debugf("[TRADECLSEVT] cancelled: sender=%d target=%d", currentCharacterID, peerContext.Char.Id)
		return
	}

	sendTradeEvent(peerSession, currentCharacterID, event)
	if bothSubmitted {
		sendTradeEvent(currentSession, peerContext.Char.Id, tradeResultRequestList)
		sendTradeEvent(peerSession, currentCharacterID, tradeResultRequestList)
	}
}

// TradeAddItem handles CSC_TRADEADDITM (264).
func TradeAddItem(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeAddItemPacketSize {
		log.Warningf("[TRADEADDITM] invalid packet size: %d", reader.Size)
		sendTradePacketResult(session, TRADE_ADD_ITEM, tradeResultInvalid)
		return
	}

	sourceSlot := reader.ReadUint16()
	tradeSlot := reader.ReadUint16()
	if sourceSlot > tradeMaxSlot || tradeSlot > tradeMaxSlot {
		sendTradePacketResult(session, TRADE_ADD_ITEM, tradeResultInvalid)
		return
	}

	currentSession, currentContext, peerSession, peerContext, ok := tradePair(session)
	if !ok {
		sendTradePacketResult(session, TRADE_ADD_ITEM, tradeResultInvalid)
		return
	}

	lockTradeContexts(currentSession, currentContext, peerSession, peerContext)
	if currentContext.Trade.Status != context.TradeStateOpen || currentContext.Trade.Submitted ||
		peerContext.Trade.Status != context.TradeStateOpen {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		sendTradePacketResult(session, TRADE_ADD_ITEM, tradeResultInvalid)
		return
	}

	item := currentContext.Char.Inventory.Get(sourceSlot)
	if item.Kind == 0 || tradeOfferExists(currentContext.Trade.Items, sourceSlot, tradeSlot) {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		sendTradePacketResult(session, TRADE_ADD_ITEM, tradeResultInvalid)
		return
	}

	peerSubmitted := peerContext.Trade.Submitted
	currentContext.Trade.Items = append(currentContext.Trade.Items, context.TradeItemOffer{
		Item: item, TradeSlot: tradeSlot,
	})
	if peerSubmitted {
		peerContext.Trade.Submitted = false
	}
	unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)

	sendTradePacketResult(session, TRADE_ADD_ITEM, tradeResultAddSuccess)
	sendTradeItem(peerSession, item, tradeSlot)
	if peerSubmitted {
		sendTradeEvent(peerSession, itemCharacterID(peerSession), tradeResultSubmitCancel)
	}
}

// TradeAddAlz handles CSC_TRADEADDALZ (265).
func TradeAddAlz(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeAddAlzPacketSize {
		log.Warningf("[TRADEADDALZ] invalid packet size: %d", reader.Size)
		sendTradePacketResult(session, TRADE_ADD_ALZ, tradeResultInvalid)
		return
	}

	amount := reader.ReadInt64()
	if amount < 0 {
		sendTradePacketResult(session, TRADE_ADD_ALZ, tradeResultInvalid)
		return
	}

	currentSession, currentContext, peerSession, peerContext, ok := tradePair(session)
	if !ok {
		sendTradePacketResult(session, TRADE_ADD_ALZ, tradeResultInvalid)
		return
	}

	lockTradeContexts(currentSession, currentContext, peerSession, peerContext)
	if currentContext.Trade.Status != context.TradeStateOpen || currentContext.Trade.Submitted ||
		peerContext.Trade.Status != context.TradeStateOpen || uint64(amount) > currentContext.Char.Alz {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		sendTradePacketResult(session, TRADE_ADD_ALZ, tradeResultInvalid)
		return
	}

	peerSubmitted := peerContext.Trade.Submitted
	currentContext.Trade.Alz = uint64(amount)
	if peerSubmitted {
		peerContext.Trade.Submitted = false
	}
	unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)

	sendTradePacketResult(session, TRADE_ADD_ALZ, tradeResultAddSuccess)
	sendTradeAlz(peerSession, uint64(amount))
	if peerSubmitted {
		sendTradeEvent(peerSession, itemCharacterID(peerSession), tradeResultSubmitCancel)
	}
}

// TradeRemoveItem handles CSC_TRADERMVITM (266).
func TradeRemoveItem(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeRemoveItemPacketSize {
		log.Warningf("[TRADERMVITM] invalid packet size: %d", reader.Size)
		sendTradePacketResult(session, TRADE_REMOVE_ITEM, tradeResultInvalid)
		return
	}

	tradeSlot := reader.ReadUint16()
	inventorySlot := reader.ReadUint16()
	currentSession, currentContext, peerSession, peerContext, ok := tradePair(session)
	if !ok {
		sendTradePacketResult(session, TRADE_REMOVE_ITEM, tradeResultInvalid)
		return
	}

	lockTradeContexts(currentSession, currentContext, peerSession, peerContext)
	if currentContext.Trade.Status != context.TradeStateOpen || currentContext.Trade.Submitted ||
		peerContext.Trade.Status != context.TradeStateOpen {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		sendTradePacketResult(session, TRADE_REMOVE_ITEM, tradeResultInvalid)
		return
	}

	index := tradeOfferIndex(currentContext.Trade.Items, tradeSlot, inventorySlot)
	if index < 0 {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		sendTradePacketResult(session, TRADE_REMOVE_ITEM, tradeResultInvalid)
		return
	}

	offer := currentContext.Trade.Items[index]
	currentContext.Trade.Items = append(currentContext.Trade.Items[:index], currentContext.Trade.Items[index+1:]...)
	peerSubmitted := peerContext.Trade.Submitted
	if peerSubmitted {
		peerContext.Trade.Submitted = false
	}
	unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)

	sendTradePacketResult(session, TRADE_REMOVE_ITEM, tradeResultRemoveSuccess)
	sendTradeRemovedItem(peerSession, offer.Item, offer.TradeSlot)
	if peerSubmitted {
		sendTradeEvent(peerSession, itemCharacterID(peerSession), tradeResultSubmitCancel)
	}
}

// TradeRequestList handles REQ_TRADEREQLST (269), the destination slots sent
// after both players submit the trade.
func TradeRequestList(session *network.Session, reader *network.Reader) {
	if reader.Size < tradeRequestListMinSize {
		log.Warningf("[TRADEREQLST] invalid packet size: %d", reader.Size)
		return
	}

	count := int(reader.ReadByte())
	if int(reader.Size) != tradeRequestListMinSize+count*2 || count > tradeMaxSlot {
		log.Warningf("[TRADEREQLST] invalid list size: size=%d count=%d", reader.Size, count)
		return
	}

	slots := make([]uint16, count)
	seen := make(map[uint16]struct{}, count)
	for i := range slots {
		slots[i] = reader.ReadUint16()
		if slots[i] > tradeMaxSlot {
			return
		}
		if _, exists := seen[slots[i]]; exists {
			return
		}
		seen[slots[i]] = struct{}{}
	}

	currentSession, currentContext, peerSession, peerContext, ok := tradePair(session)
	if !ok {
		return
	}

	lockTradeContexts(currentSession, currentContext, peerSession, peerContext)
	if currentContext.Trade.Status != context.TradeStateOpen || !currentContext.Trade.Submitted ||
		peerContext.Trade.Status != context.TradeStateOpen || !peerContext.Trade.Submitted ||
		len(slots) != len(peerContext.Trade.Items) {
		unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
		return
	}
	for _, slot := range slots {
		if currentContext.Char.Inventory.Get(slot).Kind != 0 {
			unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)
			return
		}
	}
	currentContext.Trade.DestinationSlots = slots
	ready := len(peerContext.Trade.DestinationSlots) == len(currentContext.Trade.Items)
	unlockTradeContexts(currentSession, currentContext, peerSession, peerContext)

	if ready {
		completeTrade(currentSession, currentContext, peerSession, peerContext)
	}
}

func completeTrade(firstSession *network.Session, firstContext *context.Context, secondSession *network.Session, secondContext *context.Context) {
	lockTradeContexts(firstSession, firstContext, secondSession, secondContext)
	if firstContext.Trade.Status != context.TradeStateOpen || secondContext.Trade.Status != context.TradeStateOpen ||
		!firstContext.Trade.Submitted || !secondContext.Trade.Submitted ||
		len(firstContext.Trade.DestinationSlots) != len(secondContext.Trade.Items) ||
		len(secondContext.Trade.DestinationSlots) != len(firstContext.Trade.Items) {
		unlockTradeContexts(firstSession, firstContext, secondSession, secondContext)
		return
	}

	firstItems := append([]context.TradeItemOffer(nil), firstContext.Trade.Items...)
	secondItems := append([]context.TradeItemOffer(nil), secondContext.Trade.Items...)
	firstDestinations := append([]uint16(nil), firstContext.Trade.DestinationSlots...)
	secondDestinations := append([]uint16(nil), secondContext.Trade.DestinationSlots...)
	firstCharacterID := firstContext.Char.Id
	secondCharacterID := secondContext.Char.Id
	firstAlz := firstContext.Trade.Alz
	secondAlz := secondContext.Trade.Alz
	unlockTradeContexts(firstSession, firstContext, secondSession, secondContext)

	request := inventory.TradeRequest{
		Server:          byte(g_ServerSettings.ServerId),
		FirstCharacter:  firstCharacterID,
		SecondCharacter: secondCharacterID,
		FirstAlz:        firstAlz,
		SecondAlz:       secondAlz,
	}
	request.FirstItems = make([]inventory.TradeItemTransfer, len(firstItems))
	for i, offer := range firstItems {
		request.FirstItems[i] = inventory.TradeItemTransfer{Item: offer.Item, TargetSlot: secondDestinations[i]}
	}
	request.SecondItems = make([]inventory.TradeItemTransfer, len(secondItems))
	for i, offer := range secondItems {
		request.SecondItems[i] = inventory.TradeItemTransfer{Item: offer.Item, TargetSlot: firstDestinations[i]}
	}

	response := inventory.TradeResponse{}
	if err := g_RPCHandler.Call(rpc.TradeItems, &request, &response); err != nil || !response.Result {
		log.Errorf("[TRADE] exchange failed: first=%d second=%d err=%v", firstCharacterID, secondCharacterID, err)
		abortCompletedTrade(firstSession, firstContext, secondSession, secondContext, tradeResultInvalid)
		return
	}

	lockTradeContexts(firstSession, firstContext, secondSession, secondContext)
	for _, offer := range firstItems {
		firstContext.Char.Inventory.RemoveLocal(offer.Item.Slot)
	}
	for i, offer := range secondItems {
		incoming := offer
		incoming.Item.Slot = firstDestinations[i]
		firstContext.Char.Inventory.SetLocal(incoming.Item)
	}
	for _, offer := range secondItems {
		secondContext.Char.Inventory.RemoveLocal(offer.Item.Slot)
	}
	for i, offer := range firstItems {
		incoming := offer
		incoming.Item.Slot = secondDestinations[i]
		secondContext.Char.Inventory.SetLocal(incoming.Item)
	}
	firstContext.Char.Alz = response.FirstAlz
	secondContext.Char.Alz = response.SecondAlz
	firstContext.Trade = context.TradeState{}
	secondContext.Trade = context.TradeState{}
	unlockTradeContexts(firstSession, firstContext, secondSession, secondContext)

	sendTradeEvent(firstSession, secondCharacterID, tradeResultCompleted)
	sendTradeEvent(secondSession, firstCharacterID, tradeResultCompleted)
}

func abortCompletedTrade(firstSession *network.Session, firstContext *context.Context, secondSession *network.Session, secondContext *context.Context, result byte) {
	lockTradeContexts(firstSession, firstContext, secondSession, secondContext)
	firstCharacterID := firstContext.Char.Id
	secondCharacterID := secondContext.Char.Id
	firstContext.Trade = context.TradeState{}
	secondContext.Trade = context.TradeState{}
	unlockTradeContexts(firstSession, firstContext, secondSession, secondContext)
	sendTradeEvent(firstSession, secondCharacterID, result)
	sendTradeEvent(secondSession, firstCharacterID, result)
}

// CancelTrade clears a pending or opened trade when a character leaves the
// world. The remaining client must receive TR_RESETS as well; otherwise its
// local trade window and the server-side TradeState stay out of sync.
func CancelTrade(session *network.Session) {
	currentContext, err := context.Parse(session)
	if err != nil {
		return
	}

	currentContext.Mutex.RLock()
	tradeState := currentContext.Trade
	currentContext.Mutex.RUnlock()
	if tradeState.Status == context.TradeStateNone {
		return
	}

	peerSession := g_NetworkManager.GetSession(tradeState.OpponentUserIdx)
	if peerSession == nil || peerSession.UserIdx == session.UserIdx {
		currentContext.Mutex.Lock()
		currentContext.Trade = context.TradeState{}
		currentContext.Mutex.Unlock()
		return
	}

	peerContext, err := context.Parse(peerSession)
	if err != nil {
		currentContext.Mutex.Lock()
		currentContext.Trade = context.TradeState{}
		currentContext.Mutex.Unlock()
		return
	}

	lockTradeContexts(session, currentContext, peerSession, peerContext)
	currentCharacterID := int32(0)
	if currentContext.Char != nil {
		currentCharacterID = currentContext.Char.Id
	}
	peerCharacterID := int32(0)
	if peerContext.Char != nil {
		peerCharacterID = peerContext.Char.Id
	}

	// Do not reset a different trade that may have replaced the old one on
	// the peer. The leaving character is always reset locally.
	peerLinked := peerContext.Trade.OpponentUserIdx == session.UserIdx
	currentContext.Trade = context.TradeState{}
	if peerLinked {
		peerContext.Trade = context.TradeState{}
	}
	unlockTradeContexts(session, currentContext, peerSession, peerContext)

	if peerLinked {
		sendTradeEvent(peerSession, currentCharacterID, tradeResultResets)
		log.Debugf("[TRADE] reset on leave: character=%d opponent=%d", currentCharacterID, peerCharacterID)
	}
}

func tradePair(session *network.Session) (*network.Session, *context.Context, *network.Session, *context.Context, bool) {
	currentContext, err := context.Parse(session)
	if err != nil {
		return nil, nil, nil, nil, false
	}
	currentContext.Mutex.RLock()
	tradeState := currentContext.Trade
	currentContext.Mutex.RUnlock()
	if tradeState.Status != context.TradeStateOpen || tradeState.OpponentUserIdx == session.UserIdx {
		return nil, nil, nil, nil, false
	}

	peerSession := g_NetworkManager.GetSession(tradeState.OpponentUserIdx)
	if peerSession == nil {
		return nil, nil, nil, nil, false
	}
	peerContext, err := context.Parse(peerSession)
	if err != nil {
		return nil, nil, nil, nil, false
	}
	return session, currentContext, peerSession, peerContext, true
}

func lockTradeContexts(firstSession *network.Session, firstContext *context.Context, secondSession *network.Session, secondContext *context.Context) {
	if firstSession.UserIdx < secondSession.UserIdx {
		firstContext.Mutex.Lock()
		secondContext.Mutex.Lock()
	} else {
		secondContext.Mutex.Lock()
		firstContext.Mutex.Lock()
	}
}

func unlockTradeContexts(firstSession *network.Session, firstContext *context.Context, secondSession *network.Session, secondContext *context.Context) {
	if firstSession.UserIdx < secondSession.UserIdx {
		secondContext.Mutex.Unlock()
		firstContext.Mutex.Unlock()
	} else {
		firstContext.Mutex.Unlock()
		secondContext.Mutex.Unlock()
	}
}

func tradeOfferExists(items []context.TradeItemOffer, sourceSlot, tradeSlot uint16) bool {
	for _, offer := range items {
		if offer.Item.Slot == sourceSlot || offer.TradeSlot == tradeSlot {
			return true
		}
	}
	return false
}

func tradeOfferIndex(items []context.TradeItemOffer, tradeSlot, sourceSlot uint16) int {
	for i, offer := range items {
		if offer.TradeSlot == tradeSlot && offer.Item.Slot == sourceSlot {
			return i
		}
	}
	return -1
}

func itemCharacterID(session *network.Session) int32 {
	ctx, err := context.Parse(session)
	if err != nil {
		return 0
	}
	ctx.Mutex.RLock()
	defer ctx.Mutex.RUnlock()
	return ctx.Char.Id
}

func sendTradePacketResult(session *network.Session, packetType uint16, result byte) {
	pkt := network.NewWriter(packetType)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func sendTradeItem(session *network.Session, item inventory.Item, tradeSlot uint16) {
	pkt := network.NewWriter(NFY_TRADE_ADD_ITEM)
	pkt.WriteUint32(item.Kind)
	pkt.WriteInt64(int64(item.Option))
	pkt.WriteByte(byte(tradeSlot))
	pkt.WriteUint32(item.Expire)
	// NFS_TRADEADDITM has a one-byte variable tail. It is zero for regular
	// inventory items; pet metadata can be added when pet trading is enabled.
	pkt.WriteByte(0)
	session.Send(pkt)
}

func sendTradeAlz(session *network.Session, amount uint64) {
	pkt := network.NewWriter(NFY_TRADE_ADD_ITEM)
	pkt.WriteUint32(tradeAlzItemKind)
	pkt.WriteInt64(int64(amount))
	pkt.WriteByte(0)
	pkt.WriteUint32(0)
	pkt.WriteByte(0)
	session.Send(pkt)
}

func sendTradeRemovedItem(session *network.Session, item inventory.Item, tradeSlot uint16) {
	pkt := network.NewWriter(NFY_TRADE_REMOVE_ITEM)
	pkt.WriteUint32(item.Kind)
	pkt.WriteInt64(int64(item.Option))
	pkt.WriteByte(byte(tradeSlot))
	pkt.WriteUint32(item.Expire)
	session.Send(pkt)
}

func sendTradeEvent(session *network.Session, characterID int32, result byte) {
	pkt := network.NewWriter(NFY_TRADE_EVENTS)
	pkt.WriteInt32(characterID)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func sendTradeOpenResult(session *network.Session, result byte) {
	pkt := network.NewWriter(TRADE_OPEN_EVENT)
	pkt.WriteByte(result)
	session.Send(pkt)
}

func reserveTradeRequest(sender, target *network.Session, targetCharacterID, senderCharacterID int32) bool {
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

	if senderContext.Trade.Status != context.TradeStateNone ||
		targetContext.Trade.Status != context.TradeStateNone {
		return false
	}

	senderContext.Trade = context.TradeState{
		Status:              context.TradeStatePending,
		Role:                context.TradeRoleRequester,
		OpponentUserIdx:     target.UserIdx,
		OpponentCharacterID: targetCharacterID,
	}
	targetContext.Trade = context.TradeState{
		Status:              context.TradeStatePending,
		Role:                context.TradeRoleTarget,
		OpponentUserIdx:     sender.UserIdx,
		OpponentCharacterID: senderCharacterID,
	}
	return true
}
