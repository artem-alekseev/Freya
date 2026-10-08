-- Guild creation storage migration for an existing World database.
-- The same tables and the characters.guild_no column are included in
-- sql/world.sql for fresh imports.

SET @guild_no_exists := (
  SELECT COUNT(*)
  FROM information_schema.COLUMNS
  WHERE TABLE_SCHEMA = DATABASE()
    AND TABLE_NAME = 'characters'
    AND COLUMN_NAME = 'guild_no'
);
SET @guild_no_sql := IF(
  @guild_no_exists = 0,
  'ALTER TABLE `characters` ADD COLUMN `guild_no` int(10) UNSIGNED NOT NULL DEFAULT ''0'' AFTER `nation`',
  'SELECT 1'
);
PREPARE add_guild_no FROM @guild_no_sql;
EXECUTE add_guild_no;
DEALLOCATE PREPARE add_guild_no;

CREATE TABLE IF NOT EXISTS `guilds` (
  `id` int(10) UNSIGNED NOT NULL AUTO_INCREMENT,
  `name` varchar(20) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
  `master_character_id` int(11) NOT NULL,
  `guild_type` tinyint(3) UNSIGNED NOT NULL DEFAULT '0',
  `level` tinyint(3) UNSIGNED NOT NULL DEFAULT '1',
  `created` bigint(20) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_guilds_name` (`name`),
  KEY `idx_guilds_master` (`master_character_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 ROW_FORMAT=COMPACT;

CREATE TABLE IF NOT EXISTS `guild_members` (
  `guild_id` int(10) UNSIGNED NOT NULL,
  `character_id` int(11) NOT NULL,
  `group_id` int(10) UNSIGNED NOT NULL DEFAULT '0',
  `joined` bigint(20) NOT NULL,
  PRIMARY KEY (`guild_id`,`character_id`),
  UNIQUE KEY `uq_guild_members_character` (`character_id`),
  KEY `idx_guild_members_character` (`character_id`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;
