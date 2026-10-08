-- Buddy/friends storage migration for an existing World database.
-- The same table is also included in sql/world.sql for fresh imports.

CREATE TABLE IF NOT EXISTS `characters_buddies` (
  `character_id` int(11) NOT NULL,
  `buddy_id` int(11) NOT NULL,
  `group_id` tinyint(3) UNSIGNED NOT NULL DEFAULT '0',
  `memo` varchar(50) CHARACTER SET utf8 NOT NULL DEFAULT '',
  `created` bigint(20) NOT NULL,
  PRIMARY KEY (`character_id`,`buddy_id`),
  KEY `idx_buddies_buddy` (`buddy_id`,`character_id`),
  KEY `idx_buddies_group` (`character_id`,`group_id`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;
