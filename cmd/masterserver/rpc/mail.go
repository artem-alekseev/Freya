package rpc

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/models/mail"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/textcodec"
)

type mailRow struct {
	ID                int32  `db:"id"`
	SenderID          int32  `db:"sender_id"`
	SenderName        string `db:"sender_name"`
	ReceiverID        int32  `db:"receiver_id"`
	ReceiverName      string `db:"receiver_name"`
	MailType          byte   `db:"mail_type"`
	State             byte   `db:"state"`
	DeliveryTime      int64  `db:"delivery_time"`
	Title             string `db:"title"`
	Content           string `db:"content"`
	AttachmentAlz     uint64 `db:"attachment_alz"`
	AttachmentKind    uint32 `db:"attachment_kind"`
	AttachmentSerials uint32 `db:"attachment_serials"`
	AttachmentOption  int32  `db:"attachment_option"`
	AttachmentExpire  uint32 `db:"attachment_expire"`
	DeletedSender     byte   `db:"deleted_sender"`
	DeletedReceiver   byte   `db:"deleted_receiver"`
}

func mailSummary(row mailRow) mail.Summary {
	return mail.Summary{
		ID:           row.ID,
		Type:         row.MailType,
		DeliveryTime: row.DeliveryTime,
		State:        row.State,
		SenderName:   row.SenderName,
		ReceiverName: row.ReceiverName,
		Title:        row.Title,
	}
}

func mailDetail(row mailRow) mail.Detail {
	return mail.Detail{
		Alz: row.AttachmentAlz,
		Item: inventory.Item{
			Kind:    row.AttachmentKind,
			Serials: row.AttachmentSerials,
			Option:  row.AttachmentOption,
			Expire:  row.AttachmentExpire,
		},
		HasItem: row.AttachmentKind != 0,
		Content: row.Content,
	}
}

func loadMailRows(db interface {
	Select(interface{}, string, ...interface{}) error
}, query string, args ...interface{}) ([]mailRow, error) {
	rows := make([]mailRow, 0)
	if err := db.Select(&rows, query, args...); err != nil {
		return nil, err
	}
	return rows, nil
}

// LoadMailLists is used by ChatNode for both full mailbox lists and the
// incremental received-mail refresh.
func LoadMailLists(_ *rpc.Client, r *mail.ListRequest, s *mail.ListResponse) error {
	if r == nil || r.Character <= 0 {
		return errors.New("invalid mail list request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	whereReceived := "receiver_id = ? AND deleted_receiver = 0"
	argsReceived := []interface{}{r.Character}
	if r.LastMailID > 0 {
		whereReceived += " AND id > ?"
		argsReceived = append(argsReceived, r.LastMailID)
	}

	received, err := loadMailRows(db,
		"SELECT id, sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, "+
			"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire, deleted_sender, deleted_receiver "+
			"FROM characters_mail WHERE "+whereReceived+" ORDER BY id DESC LIMIT ?",
		append(argsReceived, mail.MaxMailbox)...)
	if err != nil {
		return err
	}

	s.Received = make([]mail.Summary, 0, len(received))
	for _, row := range received {
		s.Received = append(s.Received, mailSummary(row))
	}
	s.New = append([]mail.Summary(nil), s.Received...)

	if r.LastMailID == 0 {
		sent, err := loadMailRows(db,
			"SELECT id, sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, "+
				"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire, deleted_sender, deleted_receiver "+
				"FROM characters_mail WHERE sender_id = ? AND deleted_sender = 0 ORDER BY id DESC LIMIT ?",
			r.Character, mail.MaxMailbox)
		if err != nil {
			return err
		}
		s.Sent = make([]mail.Summary, 0, len(sent))
		for _, row := range sent {
			s.Sent = append(s.Sent, mailSummary(row))
		}
	}
	return nil
}

func LoadMailDetail(_ *rpc.Client, r *mail.DetailRequest, s *mail.DetailResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid mail detail request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	query := "SELECT id, sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, " +
		"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire, deleted_sender, deleted_receiver " +
		"FROM characters_mail WHERE id = ? AND "
	if r.Sent {
		query += "sender_id = ? AND deleted_sender = 0"
	} else {
		query += "receiver_id = ? AND deleted_receiver = 0"
	}

	var row mailRow
	if err := db.Get(&row, query, r.MailID, r.Character); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	s.Result = true
	s.Summary = mailSummary(row)
	s.Detail = mailDetail(row)
	return nil
}

func MarkMailRead(_ *rpc.Client, r *mail.ReadRequest, s *mail.ReadResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid mail read request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	result, err := db.Exec("UPDATE characters_mail SET state = state | ? WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0",
		mail.StateRead, r.MailID, r.Character)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	s.Result = err == nil && rows == 1
	return err
}

func SendMail(_ *rpc.Client, r *mail.SendRequest, s *mail.SendResponse) error {
	s.Result = false
	s.ResultCode = mail.SendFail
	if r == nil || r.Sender <= 0 || strings.TrimSpace(r.ReceiverName) == "" ||
		len(r.Title) > mail.MaxTitle || len(r.Content) > mail.MaxContent ||
		(r.HasItem && r.Item.Kind == 0) {
		return errors.New("invalid mail send request")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var receiver struct {
		ID   int32  `db:"id"`
		Name string `db:"name"`
	}
	if err := tx.Get(&receiver, "SELECT id, name FROM characters WHERE name = ?", r.ReceiverName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.ResultCode = mail.SendNotFoundCharacter
			return nil
		}
		return err
	}
	if r.Sender>>3 == receiver.ID>>3 {
		s.ResultCode = mail.SendSameAccount
		return nil
	}

	var mailboxCount int
	if err := tx.Get(&mailboxCount,
		"SELECT COUNT(*) FROM characters_mail WHERE receiver_id = ? AND deleted_receiver = 0",
		receiver.ID); err != nil {
		return err
	}
	if mailboxCount >= mail.MaxMailbox {
		s.ResultCode = mail.SendFullMailbox
		return nil
	}

	var sender struct {
		Name string `db:"name"`
		Alz  uint64 `db:"alz"`
	}
	if err := tx.Get(&sender, "SELECT name, alz FROM characters WHERE id = ? FOR UPDATE", r.Sender); err != nil {
		return err
	}
	s.CurrentAlz = sender.Alz

	hasAttachment := r.Alz > 0 || r.HasItem
	fee := uint64(0)
	if hasAttachment {
		fee = mail.AttachmentFee
	}
	if r.Alz > ^uint64(0)-fee || sender.Alz < r.Alz+fee {
		if sender.Alz < r.Alz {
			s.ResultCode = mail.SendNotEnoughAlz
		} else {
			s.ResultCode = mail.SendNotEnoughFee
		}
		return nil
	}
	newAlz := sender.Alz - r.Alz - fee

	if r.HasItem {
		var stored inventory.Item
		if err := tx.Get(&stored,
			"SELECT kind, serials, opt, slot, expire FROM characters_inventory "+
				"WHERE id = ? AND slot = ? AND kind = ? AND serials = ? AND opt = ? AND expire = ? FOR UPDATE",
			r.Sender, r.Item.Slot, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Expire); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				s.ResultCode = mail.SendNotAttachableItem
				return nil
			}
			return err
		}
		if _, err := tx.Exec("DELETE FROM characters_inventory WHERE id = ? AND slot = ?",
			r.Sender, r.Item.Slot); err != nil {
			return err
		}
	}

	result, err := tx.Exec(
		"INSERT INTO characters_mail (sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, "+
			"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire) "+
			"VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?)",
		r.Sender, textcodec.FromClient([]byte(sender.Name)), receiver.ID,
		textcodec.FromClient([]byte(receiver.Name)), mail.TypeUser, time.Now().Unix(),
		r.Title, r.Content, r.Alz, r.Item.Kind, r.Item.Serials, r.Item.Option, r.Item.Expire)
	if err != nil {
		return err
	}
	mailID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE characters SET alz = ? WHERE id = ?", newAlz, r.Sender); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	go notifyMailReceived(r.Server, receiver.ID)

	s.Result = true
	s.ResultCode = mail.SendOK
	s.MailID = int32(mailID)
	s.ReceiverName = receiver.Name
	s.CurrentAlz = newAlz
	return nil
}

func notifyMailReceived(serverID byte, characterID int32) {
	request := mail.NewReceivedRequest{Server: serverID, Character: characterID}
	response := mail.NewReceivedResponse{}
	if err := g_ServerManager.SendToChat(serverID, rpc.NotifyMailReceived, &request, &response); err != nil {
		// ChatServer may be offline; the mail remains in the database and will
		// be returned by the normal mailbox list/update request.
		return
	}
}

func ReceiveMailAlz(_ *rpc.Client, r *mail.ActionRequest, s *mail.ActionResponse) error {
	s.Result = false
	s.ResultCode = mail.SendFail
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid mail alz request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var row mailRow
	if err := tx.Get(&row,
		"SELECT id, sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, "+
			"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire, deleted_sender, deleted_receiver "+
			"FROM characters_mail WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0 FOR UPDATE",
		r.MailID, r.Character); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	// Alz and the item are independent attachments. Receiving one must not
	// make the other unavailable.
	if row.AttachmentAlz == 0 {
		return nil
	}
	var current uint64
	if err := tx.Get(&current, "SELECT alz FROM characters WHERE id = ? FOR UPDATE", r.Character); err != nil {
		return err
	}
	if ^uint64(0)-current < row.AttachmentAlz {
		return nil
	}
	current += row.AttachmentAlz
	if _, err := tx.Exec("UPDATE characters SET alz = ? WHERE id = ?", current, r.Character); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE characters_mail SET attachment_alz = 0, state = state | ? WHERE id = ?",
		mail.StateRead|mail.StateAlzReceived, r.MailID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = true
	s.ResultCode = mail.SendOK
	s.MailID = r.MailID
	s.CurrentAlz = current
	return nil
}

func ReceiveMailItem(_ *rpc.Client, r *mail.ActionRequest, s *mail.ActionResponse) error {
	s.Result = false
	s.ResultCode = mail.SendFail
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid mail item request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var row mailRow
	if err := tx.Get(&row,
		"SELECT id, sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, "+
			"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire, deleted_sender, deleted_receiver "+
			"FROM characters_mail WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0 FOR UPDATE",
		r.MailID, r.Character); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	// Alz and the item are independent attachments. Receiving one must not
	// make the other unavailable.
	if row.AttachmentKind == 0 {
		return nil
	}

	slot := r.Slot
	if slot == 0xFFFF {
		foundSlot := false
		for candidate := uint16(0); candidate < 256; candidate++ {
			var occupied int
			if err := tx.Get(&occupied, "SELECT COUNT(*) FROM characters_inventory WHERE id = ? AND slot = ?",
				r.Character, candidate); err != nil {
				return err
			}
			if occupied == 0 {
				slot = candidate
				foundSlot = true
				break
			}
		}
		if !foundSlot {
			return nil
		}
	}
	var occupied int
	if err := tx.Get(&occupied, "SELECT COUNT(*) FROM characters_inventory WHERE id = ? AND slot = ?",
		r.Character, slot); err != nil {
		return err
	}
	if occupied != 0 {
		return nil
	}
	item := inventory.Item{
		Kind:    row.AttachmentKind,
		Serials: row.AttachmentSerials,
		Option:  row.AttachmentOption,
		Slot:    slot,
		Expire:  row.AttachmentExpire,
	}
	if _, err := tx.Exec("INSERT INTO characters_inventory (id, kind, serials, opt, slot, expire) VALUES (?, ?, ?, ?, ?, ?)",
		r.Character, item.Kind, item.Serials, item.Option, item.Slot, item.Expire); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE characters_mail SET attachment_kind = 0, attachment_serials = 0, attachment_option = 0, attachment_expire = 0, state = state | ? WHERE id = ?",
		mail.StateRead|mail.StateItemReceived, r.MailID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = true
	s.ResultCode = mail.SendOK
	s.MailID = r.MailID
	s.Item = item
	s.HasItem = true
	return nil
}

func ReturnMail(_ *rpc.Client, r *mail.ActionRequest, s *mail.ActionResponse) error {
	s.Result = false
	s.ResultCode = mail.ReturnFail
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid mail return request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var row mailRow
	if err := tx.Get(&row,
		"SELECT id, sender_id, sender_name, receiver_id, receiver_name, mail_type, state, delivery_time, title, content, "+
			"attachment_alz, attachment_kind, attachment_serials, attachment_option, attachment_expire, deleted_sender, deleted_receiver "+
			"FROM characters_mail WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0 FOR UPDATE",
		r.MailID, r.Character); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if row.State&mail.StateLocked != 0 {
		s.ResultCode = mail.ReturnLocked
		return nil
	}

	var target struct {
		Name string `db:"name"`
	}
	if err := tx.Get(&target, "SELECT name FROM characters WHERE id = ?", row.SenderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.ResultCode = mail.ReturnNotFoundCharacter
			return nil
		}
		return err
	}
	var mailboxCount int
	if err := tx.Get(&mailboxCount,
		"SELECT COUNT(*) FROM characters_mail WHERE receiver_id = ? AND deleted_receiver = 0",
		row.SenderID); err != nil {
		return err
	}
	if mailboxCount >= mail.MaxMailbox {
		s.ResultCode = mail.ReturnFullMailbox
		return nil
	}
	if _, err := tx.Exec(
		"UPDATE characters_mail SET sender_id = ?, sender_name = ?, receiver_id = ?, receiver_name = ?, mail_type = ?, state = 0, deleted_sender = 0, deleted_receiver = 0 WHERE id = ?",
		row.ReceiverID, row.ReceiverName, row.SenderID, target.Name, mail.TypeReturned, row.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	go notifyMailReceived(r.Server, row.SenderID)
	s.Result = true
	s.ResultCode = mail.ReturnOK
	s.MailID = row.ID
	s.SentMailID = row.ID
	s.ReceiverName = target.Name
	return nil
}

func DeleteReceivedMail(_ *rpc.Client, r *mail.ActionRequest, s *mail.ActionResponse) error {
	s.Result = false
	s.ResultCode = mail.DeleteFail
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid received mail delete request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	var alz uint64
	var kind uint32
	if err := db.QueryRow("SELECT attachment_alz, attachment_kind FROM characters_mail WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0",
		r.MailID, r.Character).Scan(&alz, &kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if alz != 0 || kind != 0 {
		s.ResultCode = mail.DeleteExistingAttachment
		return nil
	}
	result, err := db.Exec("UPDATE characters_mail SET deleted_receiver = 1 WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0",
		r.MailID, r.Character)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return nil
	}
	cleanupDeletedMail(db, r.MailID)
	s.Result = true
	s.ResultCode = mail.DeleteOK
	s.MailID = r.MailID
	return nil
}

func DeleteSentMail(_ *rpc.Client, r *mail.ActionRequest, s *mail.ActionResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid sent mail delete request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	result, err := db.Exec("UPDATE characters_mail SET deleted_sender = 1 WHERE id = ? AND sender_id = ? AND deleted_sender = 0",
		r.MailID, r.Character)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return nil
	}
	cleanupDeletedMail(db, r.MailID)
	s.Result = true
	s.MailID = r.MailID
	return nil
}

func ReportMail(_ *rpc.Client, r *mail.ActionRequest, s *mail.ActionResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.MailID <= 0 {
		return errors.New("invalid mail report request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}
	var exists int
	if err := db.Get(&exists, "SELECT COUNT(*) FROM characters_mail WHERE id = ? AND receiver_id = ? AND deleted_receiver = 0",
		r.MailID, r.Character); err != nil {
		return err
	}
	s.Result = exists == 1
	s.MailID = r.MailID
	return nil
}

func cleanupDeletedMail(db interface {
	Exec(string, ...interface{}) (sql.Result, error)
}, id int32) {
	_, _ = db.Exec("DELETE FROM characters_mail WHERE id = ? AND deleted_sender = 1 AND deleted_receiver = 1", id)
}
