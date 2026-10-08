package rpc

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ubis/Freya/share/models/guild"
	"github.com/ubis/Freya/share/rpc"
	"github.com/ubis/Freya/share/textcodec"
)

// CreateGuild creates the guild and its first member in one transaction.
// The character row is locked so two simultaneous create requests cannot both
// pass the no-guild check.
func CreateGuild(_ *rpc.Client, r *guild.CreateRequest, s *guild.CreateResponse) error {
	s.GuildNo = 0
	s.Result = guild.CreateInvalidParam
	if r == nil || r.Server == 0 || r.Character <= 0 || r.Name == "" ||
		len(textcodec.ToClient(r.Name)) > guild.MaxNameBytes {
		return nil
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

	var character struct {
		Level   uint16 `db:"level"`
		GuildNo uint32 `db:"guild_no"`
	}
	if err := tx.Get(&character,
		"SELECT level, guild_no FROM characters WHERE id = ? FOR UPDATE", r.Character); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.Result = guild.CreateInvalidParam
			return nil
		}
		return err
	}
	if character.GuildNo != 0 {
		s.Result = guild.CreateHasGuild
		return nil
	}
	if character.Level < guild.MinCreateLevel {
		s.Result = guild.CreateCharacterLevel
		return nil
	}

	var exists int
	if err := tx.Get(&exists, "SELECT COUNT(*) FROM guilds WHERE name = ?", r.Name); err != nil {
		return err
	}
	if exists != 0 {
		s.Result = guild.CreateGuildExists
		return nil
	}

	result, err := tx.Exec(
		"INSERT INTO guilds (name, master_character_id, created) VALUES (?, ?, ?)",
		r.Name, r.Character, time.Now().Unix(),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			s.Result = guild.CreateGuildExists
			return nil
		}
		return err
	}
	guildID, err := result.LastInsertId()
	if err != nil || guildID <= 0 || guildID > int64(^uint32(0)) {
		if err != nil {
			return err
		}
		return errors.New("invalid guild id returned by database")
	}

	if _, err := tx.Exec(
		"INSERT INTO guild_members (guild_id, character_id, group_id, joined) VALUES (?, ?, ?, ?)",
		guildID, r.Character, 0, time.Now().Unix(),
	); err != nil {
		return err
	}
	result, err = tx.Exec(
		"UPDATE characters SET guild_no = ? WHERE id = ? AND guild_no = 0",
		guildID, r.Character,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		s.Result = guild.CreateHasGuild
		return nil
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	s.GuildNo = uint32(guildID)
	s.Result = guild.CreateOK
	return nil
}
