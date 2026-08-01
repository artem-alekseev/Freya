package rpc

import (
	"errors"
	"time"

	"github.com/ubis/Freya/share/log"
	"github.com/ubis/Freya/share/models/character"
	"github.com/ubis/Freya/share/models/inventory"
	"github.com/ubis/Freya/share/models/skills"
	"github.com/ubis/Freya/share/rpc"

	"github.com/jmoiron/sqlx"
)

// LoadCharacters RPC Call
func LoadCharacters(_ *rpc.Client, r *character.ListReq, s *character.ListRes) error {
	var db = g_DatabaseManager.Get(r.Server)
	var res = character.ListRes{List: make([]character.Character, 0, 6)}

	if db == nil {
		*s = res
		return nil
	}

	var rows, err = db.Queryx(
		"SELECT "+
			"id, name, level, world, x, y, alz, nation, sword_rank, magic_rank, "+
			"sword_exp, magic_exp, sword_point, magic_point, sword_rank_exp, magic_rank_exp, "+
			"current_hp, max_hp, current_mp, max_mp, current_sp, max_sp, str_stat, "+
			"int_stat, dex_stat, pnt_stat, exp, war_exp, created "+
			"FROM characters "+
			"WHERE id >= ? AND id <= ?", r.Account*8, r.Account*8+5)

	if err != nil {
		log.Error("[DATABASE]", err)
		*s = res
		return nil
	}

	// iterate over each row
	for rows.Next() {
		var c = character.Character{}
		var err = rows.StructScan(&c)

		if err == nil {
			c.Style = LoadStyle(db, c.Id)
			expectedRank := character.ClassRankForLevel(c.Level)
			if c.Style.MasteryLevel != expectedRank {
				if _, rankErr := db.Exec(
					"UPDATE characters SET `rank` = ? WHERE id = ?",
					expectedRank, c.Id,
				); rankErr != nil {
					log.Errorf("[DATABASE] unable to synchronize class rank for character %d: %s", c.Id, rankErr)
				}
				c.Style.MasteryLevel = expectedRank
			}
			c.Equipment = LoadEquipment(db, c.Id)

			res.List = append(res.List, c)
		} else {
			log.Error("[DATABASE]", err)
			*s = res
			return nil
		}
	}

	// load metadata
	db.Get(&res.SlotOrder,
		"SELECT slot_order FROM lobby_metadata WHERE id = ?", r.Account)
	db.Get(&res.LastId, "SELECT last_char FROM lobby_metadata WHERE id = ?", r.Account)

	*s = res
	return nil
}

// LoadStyle Database Call
func LoadStyle(db *sqlx.DB, id int32) character.Style {
	var style = character.Style{}
	var err = db.Get(&style,
		"SELECT battle_style, `rank`, face, color, hair, aura, gender, show_helmet "+
			"FROM characters "+
			"WHERE id = ?", id)

	if err != nil {
		log.Error("[DATABASE]", err)
	}

	return style
}

// LoadEquipment Database Call
func LoadEquipment(db *sqlx.DB, id int32) inventory.Equipment {
	var equip = inventory.Equipment{}
	equip.Init()

	var rows, err = db.Queryx(
		"SELECT kind, serials, opt, slot, expire "+
			"FROM characters_equipment "+
			"WHERE id = ?", id)

	// iterate over each row
	for rows.Next() {
		var i = inventory.Item{}
		var err2 = rows.StructScan(&i)

		if err2 == nil {
			equip.Set(i.Slot, i)
		} else {
			log.Error("[DATABASE]", err2)
			return equip
		}
	}

	if err != nil {
		log.Error("[DATABASE]", err)
	}

	return equip
}

// CreateCharacter RPC Call
func CreateCharacter(_ *rpc.Client, r *character.CreateReq, s *character.CreateRes) error {
	var db = g_DatabaseManager.Get(r.Server)
	var res = character.CreateRes{}

	var c = r.Character
	var cs = c.Style

	// check name
	var name = 0
	db.Get(&name, "SELECT id FROM characters WHERE name = ? LIMIT 1", c.Name)
	if name > 0 {
		res.Result = character.NameInUse
		*s = res
		return nil
	}

	// check battle style
	if cs.BattleStyle > 6 {
		res.Result = character.DBError
		*s = res
		return nil
	}

	// get initial data
	var init = g_DataLoader.BattleStyles[cs.BattleStyle-1]
	var l = init.Location
	var st = init.Stats

	// set data
	c.World = byte(l["world"])
	c.X = byte(l["x"])
	c.Y = byte(l["y"])
	c.Level = 1
	c.SwordRank = 1
	c.MagicRank = 1
	c.CurrentHP = 0
	c.MaxHP = 0
	c.CurrentMP = uint16(st["mp"])
	c.MaxMP = uint16(st["mp"])
	c.STR = uint32(st["str"])
	c.INT = uint32(st["int"])
	c.DEX = uint32(st["dex"])
	c.RecalculateHP()
	c.RecalculateMP()
	c.Created = time.Now()

	var sql = "INSERT INTO characters ("
	sql += "id, name, world, x, y, gender, hair, color, face, battle_style, current_hp,"
	sql += "max_hp, current_mp, max_mp, str_stat, int_stat, dex_stat, created"
	sql += ") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	db.MustExec(sql, c.Id, c.Name, c.World, c.X, c.Y, cs.Gender, cs.HairStyle,
		cs.HairColor, cs.Face, cs.BattleStyle, c.CurrentHP, c.MaxHP, c.CurrentMP,
		c.MaxMP, c.STR, c.INT, c.DEX, time.Now(),
	)

	// create equipment
	c.Equipment = inventory.Equipment{}
	c.Equipment.Init()

	for key, value := range init.Equipment {
		value.Slot = inventory.MapEquipment(key)
		c.Equipment.Set(value.Slot, value)
		db.MustExec("INSERT INTO characters_equipment "+
			"(id, kind, serials, opt, slot, expire) VALUES (?, ?, ?, ?, ?, ?)",
			c.Id, value.Kind, value.Serials, value.Option, value.Slot, value.Expire,
		)
	}

	// create inventory
	for key, value := range init.Inventory {
		value.Slot = uint16(key)
		db.MustExec("INSERT INTO characters_inventory "+
			"(id, kind, serials, opt, slot, expire) VALUES (?, ?, ?, ?, ?, ?)",
			c.Id, value.Kind, value.Serials, value.Option, value.Slot, value.Expire,
		)
	}

	// create skills
	for key, value := range init.Skills {
		value.Slot = uint16(key)
		db.MustExec("INSERT INTO characters_skills "+
			"(id, skill, level, slot) VALUES (?, ?, ?, ?)",
			c.Id, value.Id, value.Level, value.Slot,
		)
	}

	// create skill links
	for key, value := range init.Links {
		value.Slot = uint16(key)
		db.MustExec("INSERT INTO characters_quickslots "+
			"(id, skill, slot) VALUES (?, ?, ?)",
			c.Id, value.Skill, value.Slot,
		)
	}

	// create lobby metadata if doesn't exist
	var account = r.Id >> 3
	db.MustExec("INSERT IGNORE INTO lobby_metadata (id) VALUE (?)", account)

	res.Result = character.Success
	res.Character = c

	*s = res
	return nil
}

// DeleteCharacter RPC Call
func DeleteCharacter(_ *rpc.Client, r *character.DeleteReq, s *character.DeleteRes) error {
	var db = g_DatabaseManager.Get(r.Server)
	var res = character.DeleteRes{}

	if db == nil {
		*s = res
		return nil
	}

	db.MustExec("DELETE FROM characters_equipment WHERE id = ?", r.CharId)
	db.MustExec("DELETE FROM characters_inventory WHERE id = ?", r.CharId)
	db.MustExec("DELETE FROM characters_warehouse WHERE id = ?", r.CharId)
	db.MustExec("DELETE FROM characters_quickslots WHERE id = ?", r.CharId)
	db.MustExec("DELETE FROM characters_skills WHERE id = ?", r.CharId)
	db.MustExec("DELETE FROM characters WHERE id = ?", r.CharId)

	res.Result = character.Success

	*s = res
	return nil
}

// SetSlotOrder RPC Call
func SetSlotOrder(_ *rpc.Client, r *character.SetOrderReq, s *character.SetOrderRes) error {
	var db = g_DatabaseManager.Get(r.Server)
	var res = character.SetOrderRes{}

	if db == nil {
		*s = res
		return nil
	}

	db.MustExec(
		"UPDATE lobby_metadata SET slot_order = ? WHERE id = ?", r.Order, r.Account)

	res.Result = true

	*s = res
	return nil
}

// LoadCharacterData RPC Call
func LoadCharacterData(c *rpc.Client, r *character.DataReq, s *character.DataRes) error {
	var db = g_DatabaseManager.Get(r.Server)
	var res = character.DataRes{}

	if db == nil {
		*s = res
		return nil
	}

	// load data
	res.Inventory = LoadInventory(db, r.Id)
	res.Warehouse = LoadWarehouse(db, r.Id)
	res.Skills = LoadSkills(db, r.Id)
	res.Links = LoadLinks(db, r.Id)

	*s = res
	return nil
}

// SaveExperience persists a character's exp and level with optimistic locking.
func SaveExperience(_ *rpc.Client, r *character.ExperienceReq, s *character.ExperienceRes) error {
	s.Result = false
	if r.Character <= 0 || r.Level == 0 || r.Level < r.ExpectedLevel || r.Exp < r.ExpectedExp {
		return errors.New("invalid experience update")
	}
	hasVitals := r.MaxHP != 0 || r.MaxMP != 0
	if hasVitals && (r.MaxHP == 0 || r.CurrentHP > r.MaxHP ||
		r.MaxMP == 0 || r.CurrentMP > r.MaxMP) {
		return errors.New("invalid vitals update")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	classRank := character.ClassRankForLevel(r.Level)
	query := "UPDATE characters SET exp = ?, level = ?, `rank` = ?, pnt_stat = pnt_stat + ?"
	args := []interface{}{r.Exp, r.Level, classRank, r.StatPoints}
	if r.MaxHP != 0 {
		query += ", current_hp = ?, max_hp = ?, current_mp = ?, max_mp = ?"
		args = append(args, r.CurrentHP, r.MaxHP, r.CurrentMP, r.MaxMP)
	}
	query += " WHERE id = ? AND exp = ? AND level = ?"
	args = append(args, r.Character, r.ExpectedExp, r.ExpectedLevel)

	result, err := db.Exec(query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	s.Result = rows == 1
	return nil
}

// SaveVitals persists the current and maximum HP/MP after a runtime
// recalculation.
func SaveVitals(_ *rpc.Client, r *character.VitalsRequest, s *character.VitalsResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.MaxHP == 0 || r.CurrentHP > r.MaxHP ||
		r.MaxMP == 0 || r.CurrentMP > r.MaxMP {
		return errors.New("invalid character vitals update")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	if _, err := db.Exec(
		"UPDATE characters SET current_hp = ?, max_hp = ?, current_mp = ?, max_mp = ? WHERE id = ?",
		r.CurrentHP, r.MaxHP, r.CurrentMP, r.MaxMP, r.Character,
	); err != nil {
		return err
	}

	s.Result = true
	return nil
}

// SaveStat atomically spends one free stat point on STR, DEX, or INT.
func SaveStat(_ *rpc.Client, r *character.StatRequest, s *character.StatResponse) error {
	s.Result = false
	if r == nil || r.Character <= 0 || r.Stat > 2 {
		return errors.New("invalid stat update")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	statColumn := ""
	switch r.Stat {
	case 0:
		statColumn = "str_stat"
	case 1:
		statColumn = "dex_stat"
	case 2:
		statColumn = "int_stat"
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var current struct {
		STR uint32 `db:"str_stat"`
		DEX uint32 `db:"dex_stat"`
		INT uint32 `db:"int_stat"`
		PNT uint32 `db:"pnt_stat"`
	}
	if err := tx.Get(&current,
		"SELECT str_stat, dex_stat, int_stat, pnt_stat FROM characters WHERE id = ? FOR UPDATE",
		r.Character); err != nil {
		return err
	}

	s.STR = current.STR
	s.DEX = current.DEX
	s.INT = current.INT
	s.PNT = current.PNT
	if current.PNT == 0 || current.PNT != r.ExpectedPNT {
		return nil
	}

	if _, err := tx.Exec(
		"UPDATE characters SET "+statColumn+" = "+statColumn+" + 1, pnt_stat = pnt_stat - 1 WHERE id = ?",
		r.Character); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	switch r.Stat {
	case 0:
		s.STR++
	case 1:
		s.DEX++
	case 2:
		s.INT++
	}
	s.PNT--
	s.Result = true
	return nil
}

// SavePosition persists the last known character position.
func SavePosition(_ *rpc.Client, r *character.PositionReq, s *character.PositionRes) error {
	s.Result = false
	if r == nil || r.Character <= 0 {
		return errors.New("invalid character position update")
	}

	db := g_DatabaseManager.Get(r.Server)
	if db == nil {
		return errors.New("game database is not configured")
	}

	_, err := db.Exec(
		"UPDATE characters SET world = ?, x = ?, y = ? WHERE id = ?",
		r.World, r.X, r.Y, r.Character,
	)
	if err != nil {
		return err
	}
	// MySQL reports zero affected rows when the position did not change.
	s.Result = true
	return nil
}

// LoadInventory Database Call
func LoadInventory(db *sqlx.DB, id int32) inventory.Inventory {
	var inv = inventory.Inventory{}
	inv.Init()

	var rows, err = db.Queryx(
		"SELECT kind, serials, opt, slot, expire "+
			"FROM characters_inventory "+
			"WHERE id = ?", id)

	// iterate over each row
	for rows.Next() {
		var i = inventory.Item{}
		var err2 = rows.StructScan(&i)

		if err2 == nil {
			inv.Set(i.Slot, i)
		} else {
			log.Error("[DATABASE]", err2)
			return inv
		}
	}

	if err != nil {
		log.Error("[DATABASE]", err)
	}

	return inv
}

// LoadWarehouse loads persistent warehouse items for a character.
func LoadWarehouse(db *sqlx.DB, id int32) inventory.Inventory {
	var warehouse = inventory.Inventory{}
	warehouse.Init()

	var rows, err = db.Queryx(
		"SELECT kind, serials, opt, slot, expire "+
			"FROM characters_warehouse "+
			"WHERE id = ?", id)
	if err != nil {
		log.Error("[DATABASE]", err)
		return warehouse
	}
	defer rows.Close()

	for rows.Next() {
		var i = inventory.Item{}
		var err2 = rows.StructScan(&i)

		if err2 == nil {
			warehouse.Set(i.Slot, i)
		} else {
			log.Error("[DATABASE]", err2)
			return warehouse
		}
	}

	if err = rows.Err(); err != nil {
		log.Error("[DATABASE]", err)
	}

	return warehouse
}

// LoadSkills Database Call
func LoadSkills(db *sqlx.DB, id int32) skills.SkillList {
	var list = skills.SkillList{}
	list.Init()

	var rows, err = db.Queryx(
		"SELECT skill, level, slot "+
			"FROM characters_skills "+
			"WHERE id = ?", id)

	// iterate over each row
	for rows.Next() {
		var s = skills.Skill{}
		var err2 = rows.StructScan(&s)

		if err2 == nil {
			list.Set(s.Slot, s)
		} else {
			log.Error("[DATABASE]", err2)
			return list
		}
	}

	if err != nil {
		log.Error("[DATABASE]", err)
	}

	return list
}

// LoadLinks Database Call
func LoadLinks(db *sqlx.DB, id int32) skills.Links {
	var list = skills.Links{}
	list.Init()

	var rows, err = db.Queryx(
		"SELECT skill, slot "+
			"FROM characters_quickslots "+
			"WHERE id = ?", id)

	// iterate over each row
	for rows.Next() {
		var l = skills.Link{}
		var err2 = rows.StructScan(&l)

		if err2 == nil {
			list.Set(l.Slot, l)
		} else {
			log.Error("[DATABASE]", err2)
			return list
		}
	}

	if err != nil {
		log.Error("[DATABASE]", err)
	}

	return list
}
