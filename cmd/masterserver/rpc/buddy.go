package rpc

import (
	"errors"
	"time"

	"github.com/ubis/Freya/share/models/buddy"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/textcodec"
)

type buddyCharacterRow struct {
	ID          int32  `db:"id"`
	Name        string `db:"name"`
	Level       byte   `db:"level"`
	BattleStyle byte   `db:"battle_style"`
	Nation      byte   `db:"nation"`
	ClassLevel  byte   `db:"rank"`
	GuildNo     uint32 `db:"guild_no"`
}

type buddyMemberRow struct {
	ID          int32  `db:"id"`
	Name        string `db:"name"`
	Level       byte   `db:"level"`
	BattleStyle byte   `db:"battle_style"`
	Nation      byte   `db:"nation"`
	ClassLevel  byte   `db:"rank"`
	GuildNo     uint32 `db:"guild_no"`
	Group       uint32 `db:"group_id"`
	Memo        string `db:"memo"`
}

func buddyCharacter(row buddyCharacterRow) buddy.Character {
	return buddy.Character{
		ID:          row.ID,
		Name:        row.Name,
		Level:       row.Level,
		BattleStyle: row.BattleStyle,
		Nation:      row.Nation,
		ClassLevel:  row.ClassLevel,
		GuildNo:     row.GuildNo,
	}
}

func LookupCharacter(_ *rpc.Client, r *buddy.LookupRequest, s *buddy.LookupResponse) error {
	if r == nil || r.Server == 0 || (r.CharacterID <= 0 && r.Name == "") {
		return errors.New("invalid buddy character lookup")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	var row buddyCharacterRow
	query := "SELECT id, name, level, battle_style, nation, `rank`, guild_no FROM characters WHERE "
	var arg interface{}
	if r.CharacterID > 0 {
		query += "id = ?"
		arg = r.CharacterID
	} else {
		query += "name = ?"
		arg = r.Name
	}
	if err := db.Get(&row, query, arg); err != nil {
		s.Found = false
		return nil
	}
	s.Found = true
	s.Character = buddyCharacter(row)
	return nil
}

func LoadBuddyList(_ *rpc.Client, r *buddy.ListRequest, s *buddy.ListResponse) error {
	if r == nil || r.Server == 0 || r.Character <= 0 {
		return errors.New("invalid buddy list request")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	rows := make([]buddyMemberRow, 0)
	if err := db.Select(&rows,
		"SELECT c.id, c.name, c.level, c.battle_style, c.nation, c.rank, c.guild_no, b.group_id, b.memo "+
			"FROM characters_buddies b JOIN characters c ON c.id = b.buddy_id "+
			"WHERE b.character_id = ? ORDER BY b.group_id, c.name",
		r.Character); err != nil {
		return err
	}

	s.Members = make([]buddy.Member, 0, len(rows))
	for _, row := range rows {
		s.Members = append(s.Members, buddy.Member{
			Character: buddyCharacter(buddyCharacterRow{
				ID: row.ID, Name: row.Name, Level: row.Level,
				BattleStyle: row.BattleStyle, Nation: row.Nation, ClassLevel: row.ClassLevel,
				GuildNo: row.GuildNo,
			}),
			Group: row.Group,
			Memo:  row.Memo,
		})
	}
	return nil
}

func AddBuddy(_ *rpc.Client, r *buddy.AddRequest, s *buddy.AddResponse) error {
	s.Result = buddy.ResultInvalidParam
	if r == nil || r.Server == 0 || r.Requester <= 0 || r.Target <= 0 || r.Requester == r.Target {
		return errors.New("invalid buddy add request")
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

	var characterCount int
	if err := tx.Get(&characterCount, "SELECT COUNT(*) FROM characters WHERE id IN (?, ?)", r.Requester, r.Target); err != nil {
		return err
	}
	if characterCount != 2 {
		s.Result = buddy.ResultNotExist
		return nil
	}

	var already int
	if err := tx.Get(&already,
		"SELECT COUNT(*) FROM characters_buddies WHERE character_id = ? AND buddy_id = ?",
		r.Requester, r.Target); err != nil {
		return err
	}
	if already != 0 {
		s.Result = buddy.ResultAlreadyMember
		return nil
	}

	var requesterCount, targetCount int
	if err := tx.Get(&requesterCount, "SELECT COUNT(*) FROM characters_buddies WHERE character_id = ?", r.Requester); err != nil {
		return err
	}
	if err := tx.Get(&targetCount, "SELECT COUNT(*) FROM characters_buddies WHERE character_id = ?", r.Target); err != nil {
		return err
	}
	if requesterCount >= buddy.MaxMembers {
		s.Result = buddy.ResultMaxMember
		return nil
	}
	if targetCount >= buddy.MaxMembers {
		s.Result = buddy.ResultMaxMemberTarget
		return nil
	}

	created := time.Now().Unix()
	if _, err := tx.Exec(
		"INSERT INTO characters_buddies (character_id, buddy_id, group_id, memo, created) VALUES (?, ?, ?, ?, ?), (?, ?, ?, ?, ?)",
		r.Requester, r.Target, buddy.DefaultGroup, "", created,
		r.Target, r.Requester, buddy.DefaultGroup, "", created); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = buddy.ResultOK
	return nil
}

// UpdateBuddyMemo changes the memo in the owner's directed buddy row. Memos
// are intentionally not mirrored: each character has its own note for a
// friend, just as in WorldSvr.
func UpdateBuddyMemo(_ *rpc.Client, r *buddy.MemoModifyRequest, s *buddy.MemoModifyResponse) error {
	s.Result = buddy.MemoModifyInvalidParam
	if r == nil || r.Server == 0 || r.Character <= 0 || r.Target <= 0 ||
		r.Character == r.Target || len(textcodec.ToClient(r.Memo)) > 50 {
		return errors.New("invalid buddy memo update")
	}
	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	var exists int
	if err := db.Get(&exists,
		"SELECT COUNT(*) FROM characters_buddies WHERE character_id = ? AND buddy_id = ?",
		r.Character, r.Target); err != nil {
		return err
	}
	if exists != 1 {
		s.Result = buddy.MemoModifyInvalidReturn
		return nil
	}

	if _, err := db.Exec(
		"UPDATE characters_buddies SET memo = ? WHERE character_id = ? AND buddy_id = ?",
		r.Memo, r.Character, r.Target); err != nil {
		return err
	}
	s.Result = buddy.MemoModifyOK
	s.Memo = r.Memo
	return nil
}

// DeleteBuddy removes both directed rows of a friendship atomically. The
// client sends the operation only for its own row, but WorldSvr also removes
// the corresponding row from the other character's list.
func DeleteBuddy(_ *rpc.Client, r *buddy.DeleteRequest, s *buddy.DeleteResponse) error {
	s.Result = buddy.DeleteInvalidParam
	if r == nil || r.Server == 0 || r.Character <= 0 || r.Target <= 0 ||
		r.Character == r.Target {
		return errors.New("invalid buddy delete request")
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

	var exists int
	if err := tx.Get(&exists,
		"SELECT COUNT(*) FROM characters_buddies WHERE character_id = ? AND buddy_id = ?",
		r.Character, r.Target); err != nil {
		return err
	}
	if exists != 1 {
		s.Result = buddy.DeleteNotExist
		return nil
	}

	result, err := tx.Exec(
		"DELETE FROM characters_buddies WHERE (character_id = ? AND buddy_id = ?) OR (character_id = ? AND buddy_id = ?)",
		r.Character, r.Target, r.Target, r.Character)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 2 {
		s.Result = buddy.DeleteInvalidReturn
		return nil
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.Result = buddy.DeleteOK
	return nil
}
