-- Mail storage migration for an existing World database.
-- The same table is also included in sql/world.sql for fresh imports.

CREATE TABLE IF NOT EXISTS `characters_mail` (
  `id` int(11) NOT NULL AUTO_INCREMENT,
  `sender_id` int(11) NOT NULL,
  `sender_name` varchar(17) CHARACTER SET utf8 COLLATE utf8_bin NOT NULL,
  `receiver_id` int(11) NOT NULL,
  `receiver_name` varchar(17) CHARACTER SET utf8 COLLATE utf8_bin NOT NULL,
  `mail_type` tinyint(3) UNSIGNED NOT NULL DEFAULT '1',
  `state` tinyint(3) UNSIGNED NOT NULL DEFAULT '0',
  `delivery_time` bigint(20) NOT NULL,
  `title` varchar(41) CHARACTER SET utf8 NOT NULL,
  `content` varchar(513) CHARACTER SET utf8 NOT NULL,
  `attachment_alz` bigint(20) UNSIGNED NOT NULL DEFAULT '0',
  `attachment_kind` int(10) UNSIGNED NOT NULL DEFAULT '0',
  `attachment_serials` int(10) UNSIGNED NOT NULL DEFAULT '0',
  `attachment_option` int(11) NOT NULL DEFAULT '0',
  `attachment_expire` int(10) UNSIGNED NOT NULL DEFAULT '0',
  `deleted_sender` tinyint(1) NOT NULL DEFAULT '0',
  `deleted_receiver` tinyint(1) NOT NULL DEFAULT '0',
  PRIMARY KEY (`id`),
  KEY `idx_mail_receiver` (`receiver_id`,`deleted_receiver`,`id`),
  KEY `idx_mail_sender` (`sender_id`,`deleted_sender`,`id`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;
