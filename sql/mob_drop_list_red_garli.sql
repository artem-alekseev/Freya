-- Red Garli (mob_species = 51)
-- Small Upgrade Core (item_kind = 9): 50%
-- Medium Upgrade Core (item_kind = 10): 50%

CREATE TABLE IF NOT EXISTS `mob_drop_list` (
  `id` int(10) UNSIGNED NOT NULL AUTO_INCREMENT,
  `mob_species` int(10) UNSIGNED NOT NULL,
  `item_kind` int(10) UNSIGNED NOT NULL,
  `item_option` int(11) NOT NULL DEFAULT '0',
  `amount` smallint(5) UNSIGNED NOT NULL DEFAULT '1',
  `chance_bps` smallint(5) UNSIGNED NOT NULL DEFAULT '10000',
  `enabled` tinyint(1) NOT NULL DEFAULT '1',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uq_mob_drop_rule` (`mob_species`,`item_kind`,`item_option`,`amount`,`chance_bps`),
  KEY `idx_mob_drop_species` (`mob_species`,`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

INSERT INTO `mob_drop_list`
  (`mob_species`, `item_kind`, `item_option`, `amount`, `chance_bps`, `enabled`)
VALUES (51, 9, 0, 1, 5000, 1)
ON DUPLICATE KEY UPDATE
  `item_option` = VALUES(`item_option`),
  `amount` = VALUES(`amount`),
  `chance_bps` = VALUES(`chance_bps`),
  `enabled` = VALUES(`enabled`);

INSERT INTO `mob_drop_list`
  (`mob_species`, `item_kind`, `item_option`, `amount`, `chance_bps`, `enabled`)
VALUES (51, 10, 0, 1, 5000, 1)
ON DUPLICATE KEY UPDATE
  `item_option` = VALUES(`item_option`),
  `amount` = VALUES(`amount`),
  `chance_bps` = VALUES(`chance_bps`),
  `enabled` = VALUES(`enabled`);
