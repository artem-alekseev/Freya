package packet

import (
	"bytes"
	"strings"

	"github.com/ubis/Freya/cmd/gameserver/context"
	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/models/mail"
	"github.com/ubis/Freya/share/network"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/textcodec"
)

const (
	mailDetailPacketSize     = 14
	mailActionPacketSize     = 14
	mailReceiveItemSize      = 18
	mailSendPrefixPacketSize = 86 // header + fixed MailForm fields + content length
	mailFixedNameSize        = 17
	mailFixedTitleSize       = 41
)

func mailContext(session *network.Session) (*context.Context, bool) {
	ctx, err := context.Parse(session)
	if err != nil {
		log.Error(err.Error())
		return nil, false
	}
	if ctx.Char == nil {
		return nil, false
	}
	return ctx, true
}

func sendMailDetail(session *network.Session, packetType uint16, mailID int32, result bool, detail mail.Detail) {
	pkt := network.NewWriter(packetType)
	pkt.WriteBool(result)
	pkt.WriteInt32(mailID)
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
	session.Send(pkt)
}

func MailGetReceivedDetail(session *network.Session, reader *network.Reader) {
	mailGetDetail(session, reader, false)
}

func MailGetSentDetail(session *network.Session, reader *network.Reader) {
	mailGetDetail(session, reader, true)
}

func mailGetDetail(session *network.Session, reader *network.Reader, sent bool) {
	packetType := uint16(MAIL_GET_RECEIVED_DETAIL)
	if sent {
		packetType = uint16(MAIL_GET_SENT_DETAIL)
	}
	if reader.Size < mailDetailPacketSize {
		log.Warningf("[MAILDETAIL] invalid packet size: %d", reader.Size)
		sendMailDetail(session, packetType, 0, false, mail.Detail{})
		return
	}
	mailID := reader.ReadInt32()
	ctx, ok := mailContext(session)
	if !ok {
		sendMailDetail(session, packetType, mailID, false, mail.Detail{})
		return
	}
	request := mail.DetailRequest{
		Server:    byte(g_ServerSettings.ServerId),
		Character: ctx.Char.Id,
		MailID:    mailID,
		Sent:      sent,
	}
	response := mail.DetailResponse{}
	if err := g_RPCHandler.Call(rpc.LoadMailDetail, &request, &response); err != nil {
		log.Errorf("[MAILDETAIL] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
		sendMailDetail(session, packetType, mailID, false, mail.Detail{})
		return
	}
	sendMailDetail(session, packetType, mailID, response.Result, response.Detail)
}

func MailReadReceived(session *network.Session, reader *network.Reader) {
	if reader.Size < mailDetailPacketSize {
		log.Warningf("[MAILREAD] invalid packet size: %d", reader.Size)
		return
	}
	mailID := reader.ReadInt32()
	ctx, ok := mailContext(session)
	if !ok {
		return
	}
	request := mail.ReadRequest{Server: byte(g_ServerSettings.ServerId), Character: ctx.Char.Id, MailID: mailID}
	response := mail.ReadResponse{}
	if err := g_RPCHandler.Call(rpc.MarkMailRead, &request, &response); err != nil {
		log.Errorf("[MAILREAD] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
	}
}

func MailReceiveAlz(session *network.Session, reader *network.Reader) {
	if reader.Size < mailActionPacketSize {
		log.Warningf("[MAILALZ] invalid packet size: %d", reader.Size)
		sendMailAlzResult(session, mail.SendFail, 0, 0)
		return
	}
	mailID := reader.ReadInt32()
	ctx, ok := mailContext(session)
	if !ok {
		sendMailAlzResult(session, mail.SendFail, mailID, 0)
		return
	}
	request := mail.ActionRequest{Server: byte(g_ServerSettings.ServerId), Character: ctx.Char.Id, MailID: mailID}
	response := mail.ActionResponse{}
	if err := g_RPCHandler.Call(rpc.ReceiveMailAlz, &request, &response); err != nil {
		log.Errorf("[MAILALZ] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
		sendMailAlzResult(session, mail.SendFail, mailID, 0)
		return
	}
	if response.Result {
		setCharacterAlz(ctx, response.CurrentAlz)
	}
	sendMailAlzResult(session, response.ResultCode, mailID, response.CurrentAlz)
}

func sendMailAlzResult(session *network.Session, result byte, mailID int32, currentAlz uint64) {
	pkt := network.NewWriter(MAIL_RECEIVE_ALZ)
	pkt.WriteInt32(int32(result))
	pkt.WriteInt32(mailID)
	pkt.WriteUint64(currentAlz)
	session.Send(pkt)
}

func MailReceiveItem(session *network.Session, reader *network.Reader) {
	if reader.Size < mailReceiveItemSize {
		log.Warningf("[MAILITEM] invalid packet size: %d", reader.Size)
		sendMailItemResult(session, mail.SendFail, 0, nil)
		return
	}
	mailID := reader.ReadInt32()
	slotValue := reader.ReadInt32()
	if slotValue < -1 || slotValue > int32(^uint16(0)) {
		sendMailItemResult(session, mail.SendFail, mailID, nil)
		return
	}
	requestedSlot := uint16(0xFFFF)
	if slotValue >= 0 {
		requestedSlot = uint16(slotValue)
	}
	ctx, ok := mailContext(session)
	if !ok {
		sendMailItemResult(session, mail.SendFail, mailID, nil)
		return
	}
	request := mail.ActionRequest{
		Server:    byte(g_ServerSettings.ServerId),
		Character: ctx.Char.Id,
		MailID:    mailID,
		Slot:      requestedSlot,
	}
	response := mail.ActionResponse{}
	if err := g_RPCHandler.Call(rpc.ReceiveMailItem, &request, &response); err != nil {
		log.Errorf("[MAILITEM] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
		sendMailItemResult(session, mail.SendFail, mailID, nil)
		return
	}
	if response.Result && response.HasItem {
		if ctx.Char.Inventory == nil {
			sendMailItemResult(session, mail.SendFail, mailID, nil)
			return
		}
		ctx.Char.Inventory.SetLocal(response.Item)
		sendMailItemResult(session, response.ResultCode, mailID, &response.Item)
		return
	}
	sendMailItemResult(session, response.ResultCode, mailID, nil)
}

func sendMailItemResult(session *network.Session, result byte, mailID int32, item *inventory.Item) {
	pkt := network.NewWriter(MAIL_RECEIVE_ITEM)
	pkt.WriteInt32(int32(result))
	pkt.WriteInt32(mailID)
	if item != nil {
		pkt.WriteUint32(item.Kind)
		pkt.WriteInt32(int32(item.Serials))
		pkt.WriteInt32(item.Option)
		pkt.WriteUint16(item.Slot)
		pkt.WriteUint32(item.Expire)
	}
	session.Send(pkt)
}

func MailSend(session *network.Session, reader *network.Reader) {
	if int(reader.Size) < mailSendPrefixPacketSize+1 {
		log.Warningf("[MAILSEND] invalid packet size: %d", reader.Size)
		sendMailSendResult(session, mail.SendFail, 0, "")
		return
	}

	replyID := reader.ReadInt32()
	receiverName := readFixedMailString(reader, mailFixedNameSize)
	alz := reader.ReadUint64()
	slotValue := reader.ReadInt32()
	title := textcodec.FromClient([]byte(readFixedMailString(reader, mailFixedTitleSize)))
	contentLength := int(reader.ReadUint16())
	if contentLength > mail.MaxContent || int(reader.Size) < mailSendPrefixPacketSize+contentLength+1 {
		log.Warningf("[MAILSEND] invalid content length: %d packet=%d", contentLength, reader.Size)
		sendMailSendResult(session, mail.SendFail, 0, "")
		return
	}
	content := textcodec.FromClient(bytes.TrimRight(reader.ReadBytes(contentLength+1), "\x00"))

	ctx, ok := mailContext(session)
	if !ok {
		sendMailSendResult(session, mail.SendFail, 0, "")
		return
	}
	request := mail.SendRequest{
		Server:       byte(g_ServerSettings.ServerId),
		Sender:       ctx.Char.Id,
		SenderName:   ctx.Char.Name,
		ReceiverName: receiverName,
		ReplyID:      replyID,
		Title:        title,
		Content:      content,
		Alz:          alz,
	}
	if slotValue != -1 {
		if slotValue < 0 || slotValue > int32(^uint16(0)) || ctx.Char.Inventory == nil {
			sendMailSendResult(session, mail.SendNotAttachableItem, 0, "")
			return
		}
		request.Item = ctx.Char.Inventory.Get(uint16(slotValue))
		if request.Item.Kind == 0 {
			sendMailSendResult(session, mail.SendNotAttachableItem, 0, "")
			return
		}
		request.HasItem = true
	}

	response := mail.SendResponse{}
	if err := g_RPCHandler.Call(rpc.SendMail, &request, &response); err != nil {
		log.Errorf("[MAILSEND] database request failed: character=%d: %s", ctx.Char.Id, err)
		sendMailSendResult(session, mail.SendFail, 0, "")
		return
	}
	if response.Result {
		setCharacterAlz(ctx, response.CurrentAlz)
		if request.HasItem {
			ctx.Char.Inventory.RemoveLocal(request.Item.Slot)
		}
	}
	sendMailSendResult(session, response.ResultCode, response.MailID, response.ReceiverName)
}

func sendMailSendResult(session *network.Session, result byte, mailID int32, receiverName string) {
	pkt := network.NewWriter(MAIL_SEND)
	pkt.WriteInt32(int32(result))
	pkt.WriteInt32(mailID)
	writeFixedMailString(pkt, receiverName, mailFixedNameSize)
	session.Send(pkt)
}

func MailReturn(session *network.Session, reader *network.Reader) {
	mailID, ok := readMailActionID(session, reader, "MAILRETURN")
	if !ok {
		return
	}
	ctx, ok := mailContext(session)
	if !ok {
		sendMailReturnResult(session, mail.ReturnFail, mailID, 0, "")
		return
	}
	request := mail.ActionRequest{Server: byte(g_ServerSettings.ServerId), Character: ctx.Char.Id, MailID: mailID}
	response := mail.ActionResponse{}
	if err := g_RPCHandler.Call(rpc.ReturnMail, &request, &response); err != nil {
		log.Errorf("[MAILRETURN] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
		sendMailReturnResult(session, mail.ReturnFail, mailID, 0, "")
		return
	}
	sendMailReturnResult(session, response.ResultCode, mailID, response.SentMailID, response.ReceiverName)
}

func sendMailReturnResult(session *network.Session, result byte, mailID int32, sentMailID int32, receiverName string) {
	pkt := network.NewWriter(MAIL_RETURN)
	pkt.WriteInt32(int32(result))
	pkt.WriteInt32(mailID)
	pkt.WriteInt32(sentMailID)
	writeFixedMailString(pkt, receiverName, mailFixedNameSize)
	session.Send(pkt)
}

func MailDeleteReceived(session *network.Session, reader *network.Reader) {
	mailID, ok := readMailActionID(session, reader, "MAILDELRECEIVED")
	if !ok {
		return
	}
	ctx, ok := mailContext(session)
	if !ok {
		sendMailDeleteResult(session, MAIL_DELETE_RECEIVED, mail.DeleteFail, mailID)
		return
	}
	request := mail.ActionRequest{Server: byte(g_ServerSettings.ServerId), Character: ctx.Char.Id, MailID: mailID}
	response := mail.ActionResponse{}
	if err := g_RPCHandler.Call(rpc.DeleteReceivedMail, &request, &response); err != nil {
		log.Errorf("[MAILDELRECEIVED] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
		response.ResultCode = mail.DeleteFail
	}
	sendMailDeleteResult(session, MAIL_DELETE_RECEIVED, response.ResultCode, mailID)
}

func MailDeleteSent(session *network.Session, reader *network.Reader) {
	mailID, ok := readMailActionID(session, reader, "MAILDELSENT")
	if !ok {
		return
	}
	ctx, ok := mailContext(session)
	if !ok {
		sendMailDeleteResult(session, MAIL_DELETE_SENT, mail.DeleteFail, mailID)
		return
	}
	request := mail.ActionRequest{Server: byte(g_ServerSettings.ServerId), Character: ctx.Char.Id, MailID: mailID}
	response := mail.ActionResponse{}
	if err := g_RPCHandler.Call(rpc.DeleteSentMail, &request, &response); err != nil {
		log.Errorf("[MAILDELSENT] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
	}
	pkt := network.NewWriter(MAIL_DELETE_SENT)
	pkt.WriteBool(response.Result)
	pkt.WriteInt32(mailID)
	session.Send(pkt)
}

func sendMailDeleteResult(session *network.Session, packetType uint16, result byte, mailID int32) {
	pkt := network.NewWriter(packetType)
	pkt.WriteInt32(int32(result))
	pkt.WriteInt32(mailID)
	session.Send(pkt)
}

func MailReport(session *network.Session, reader *network.Reader) {
	mailID, ok := readMailActionID(session, reader, "MAILREPORT")
	if !ok {
		return
	}
	ctx, ok := mailContext(session)
	if !ok {
		sendMailDeleteResult(session, MAIL_REPORT, mail.DeleteFail, mailID)
		return
	}
	request := mail.ActionRequest{Server: byte(g_ServerSettings.ServerId), Character: ctx.Char.Id, MailID: mailID}
	response := mail.ActionResponse{}
	if err := g_RPCHandler.Call(rpc.ReportMail, &request, &response); err != nil {
		log.Errorf("[MAILREPORT] database request failed: character=%d mail=%d: %s", ctx.Char.Id, mailID, err)
	}
	sendMailDeleteResult(session, MAIL_REPORT, boolResult(response.Result), mailID)
}

func readMailActionID(session *network.Session, reader *network.Reader, name string) (int32, bool) {
	if reader.Size < mailActionPacketSize {
		log.Warningf("[%s] invalid packet size: %d", name, reader.Size)
		return 0, false
	}
	return reader.ReadInt32(), true
}

func readFixedMailString(reader *network.Reader, size int) string {
	return strings.TrimRight(reader.ReadString(size), "\x00")
}

func writeFixedMailString(pkt *network.Writer, value string, size int) {
	data := make([]byte, size)
	copy(data, textcodec.ToClient(value))
	pkt.WriteBytes(data)
}

func setCharacterAlz(ctx *context.Context, alz uint64) {
	ctx.Mutex.Lock()
	if ctx.Char != nil {
		ctx.Char.Alz = alz
	}
	ctx.Mutex.Unlock()
}

func boolResult(result bool) byte {
	if result {
		return 0
	}
	return 1
}
