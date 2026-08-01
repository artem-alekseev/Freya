-- phpMyAdmin SQL Dump
-- version 4.7.0
-- https://www.phpmyadmin.net/
--
-- Host: 127.0.0.1
-- Generation Time: Jul 25, 2017 at 01:17 PM
-- Server version: 10.1.25-MariaDB
-- PHP Version: 7.1.7

SET SQL_MODE = "NO_AUTO_VALUE_ON_ZERO";
SET AUTOCOMMIT = 0;
START TRANSACTION;
SET time_zone = "+00:00";


/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!40101 SET NAMES utf8mb4 */;

--
-- Database: `world`
--

-- --------------------------------------------------------

--
-- Table structure for table `characters`
--

CREATE TABLE `characters` (
  `id` int(11) NOT NULL,
  `name` varchar(32) CHARACTER SET utf8 COLLATE utf8_bin NOT NULL,
  `level` smallint(5) UNSIGNED NOT NULL DEFAULT '1',
  `world` tinyint(3) UNSIGNED NOT NULL,
  `x` tinyint(3) UNSIGNED NOT NULL,
  `y` tinyint(3) UNSIGNED NOT NULL,
  `gender` tinyint(1) NOT NULL,
  `aura` tinyint(3) UNSIGNED NOT NULL DEFAULT '0',
  `hair` tinyint(3) UNSIGNED NOT NULL,
  `color` tinyint(3) UNSIGNED NOT NULL,
  `face` tinyint(3) UNSIGNED NOT NULL,
  `rank` tinyint(3) UNSIGNED NOT NULL DEFAULT '1',
  `battle_style` tinyint(3) UNSIGNED NOT NULL,
  `show_helmet` tinyint(1) NOT NULL DEFAULT '0',
  `alz` bigint(20) UNSIGNED NOT NULL DEFAULT '0',
  `nation` tinyint(3) UNSIGNED NOT NULL DEFAULT '0',
  `sword_rank` tinyint(3) UNSIGNED NOT NULL DEFAULT '1',
  `magic_rank` tinyint(3) UNSIGNED NOT NULL DEFAULT '1',
  `sword_exp` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `magic_exp` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `sword_point` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `magic_point` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `sword_rank_exp` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `magic_rank_exp` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `current_hp` smallint(5) UNSIGNED NOT NULL,
  `max_hp` smallint(5) UNSIGNED NOT NULL,
  `current_mp` smallint(5) UNSIGNED NOT NULL,
  `max_mp` smallint(5) UNSIGNED NOT NULL,
  `current_sp` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `max_sp` smallint(5) UNSIGNED NOT NULL DEFAULT '0',
  `str_stat` int(10) UNSIGNED NOT NULL,
  `int_stat` int(10) UNSIGNED NOT NULL,
  `dex_stat` int(10) UNSIGNED NOT NULL,
  `pnt_stat` int(10) UNSIGNED NOT NULL DEFAULT '0',
  `exp` bigint(20) UNSIGNED NOT NULL DEFAULT '0',
  `war_exp` bigint(20) UNSIGNED NOT NULL DEFAULT '0',
  `created` datetime NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

-- --------------------------------------------------------

--
-- Table structure for table `characters_equipment`
--

CREATE TABLE `characters_equipment` (
  `id` int(11) NOT NULL,
  `kind` int(10) UNSIGNED NOT NULL,
  `serials` int(10) UNSIGNED NOT NULL,
  `opt` int(10) UNSIGNED NOT NULL,
  `slot` smallint(5) UNSIGNED NOT NULL,
  `expire` int(10) UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

-- --------------------------------------------------------

--
-- Table structure for table `characters_inventory`
--

CREATE TABLE `characters_inventory` (
  `id` int(11) NOT NULL,
  `kind` int(10) UNSIGNED NOT NULL,
  `serials` int(10) UNSIGNED NOT NULL,
  `opt` int(10) UNSIGNED NOT NULL,
  `slot` smallint(5) UNSIGNED NOT NULL,
  `expire` int(10) UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

-- --------------------------------------------------------

--
-- Table structure for table `characters_warehouse`
--

CREATE TABLE `characters_warehouse` (
  `id` int(11) NOT NULL,
  `kind` int(10) UNSIGNED NOT NULL,
  `serials` int(10) UNSIGNED NOT NULL,
  `opt` int(10) UNSIGNED NOT NULL,
  `slot` smallint(5) UNSIGNED NOT NULL,
  `expire` int(10) UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

-- --------------------------------------------------------

--
-- Table structure for table `mob_drop_list`
--

CREATE TABLE `mob_drop_list` (
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

-- --------------------------------------------------------

--
-- Table structure for table `characters_quickslots`
--

CREATE TABLE `characters_quickslots` (
  `id` int(11) NOT NULL,
  `skill` smallint(5) UNSIGNED NOT NULL,
  `slot` smallint(5) UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

-- --------------------------------------------------------

--
-- Table structure for table `characters_skills`
--

CREATE TABLE `characters_skills` (
  `id` int(11) NOT NULL,
  `skill` smallint(5) UNSIGNED NOT NULL,
  `level` tinyint(3) UNSIGNED NOT NULL,
  `slot` smallint(5) UNSIGNED NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=latin1 ROW_FORMAT=COMPACT;

-- --------------------------------------------------------

--
-- Table structure for table `lobby_metadata`
--

CREATE TABLE `lobby_metadata` (
  `id` int(11) NOT NULL,
  `last_char` int(11) NOT NULL DEFAULT '0',
  `slot_order` int(11) NOT NULL DEFAULT '0'
) ENGINE=InnoDB DEFAULT CHARSET=latin1;

--
-- Indexes for dumped tables
--

--
-- Indexes for table `characters`
--
ALTER TABLE `characters`
  ADD PRIMARY KEY (`id`);

--
-- Indexes for table `characters_equipment`
--
ALTER TABLE `characters_equipment`
  ADD PRIMARY KEY (`id`,`slot`);

--
-- Indexes for table `characters_inventory`
--
ALTER TABLE `characters_inventory`
  ADD PRIMARY KEY (`id`,`slot`);

--
-- Indexes for table `characters_warehouse`
--
ALTER TABLE `characters_warehouse`
  ADD PRIMARY KEY (`id`,`slot`);

--
-- Indexes for table `characters_quickslots`
--
ALTER TABLE `characters_quickslots`
  ADD PRIMARY KEY (`id`,`slot`);

--
-- Indexes for table `characters_skills`
--
ALTER TABLE `characters_skills`
  ADD PRIMARY KEY (`id`,`slot`);

--
-- Indexes for table `lobby_metadata`
--
ALTER TABLE `lobby_metadata`
  ADD PRIMARY KEY (`id`);

--
-- Constraints for dumped tables
--

--
-- Constraints for table `characters_equipment`
--
ALTER TABLE `characters_equipment`
  ADD CONSTRAINT `characters_equipment_ibfk_1` FOREIGN KEY (`id`) REFERENCES `characters` (`id`);

--
-- Constraints for table `characters_inventory`
--
ALTER TABLE `characters_inventory`
  ADD CONSTRAINT `characters_inventory_ibfk_1` FOREIGN KEY (`id`) REFERENCES `characters` (`id`);

--
-- Constraints for table `characters_warehouse`
--
ALTER TABLE `characters_warehouse`
  ADD CONSTRAINT `characters_warehouse_ibfk_1` FOREIGN KEY (`id`) REFERENCES `characters` (`id`);

--
-- Constraints for table `characters_quickslots`
--
ALTER TABLE `characters_quickslots`
  ADD CONSTRAINT `characters_quickslots_ibfk_1` FOREIGN KEY (`id`) REFERENCES `characters` (`id`);

--
-- Constraints for table `characters_skills`
--
ALTER TABLE `characters_skills`
  ADD CONSTRAINT `characters_skills_ibfk_1` FOREIGN KEY (`id`) REFERENCES `characters` (`id`);

-- Test warehouse items for character 16. The SELECT keeps a fresh database
-- import valid when character 16 has not been created yet.
INSERT IGNORE INTO `characters_warehouse`
  (`id`, `kind`, `serials`, `opt`, `slot`, `expire`)
SELECT 16, 3, 0, 5, 0, 0 FROM `characters` WHERE `id` = 16;

INSERT IGNORE INTO `characters_warehouse`
  (`id`, `kind`, `serials`, `opt`, `slot`, `expire`)
SELECT 16, 6, 0, 20, 1, 0 FROM `characters` WHERE `id` = 16;

INSERT IGNORE INTO `characters_warehouse`
  (`id`, `kind`, `serials`, `opt`, `slot`, `expire`)
SELECT 16, 12, 0, 5, 2, 0 FROM `characters` WHERE `id` = 16;

INSERT IGNORE INTO `characters_inventory`
  (`id`, `kind`, `serials`, `opt`, `slot`, `expire`)
SELECT 16, 659, 0, 13107220, 0, 0 FROM `characters` WHERE `id` = 16;

-- Test drop rule: mob species 63 always drops a stack of five HP potions.
INSERT INTO `mob_drop_list`
  (`mob_species`, `item_kind`, `item_option`, `amount`, `chance_bps`, `enabled`)
VALUES (63, 3, 0, 5, 10000, 1)
ON DUPLICATE KEY UPDATE `enabled` = VALUES(`enabled`);

-- Red Garli (mob 51): small and medium Upgrade Core, 50% each.
INSERT INTO `mob_drop_list`
  (`mob_species`, `item_kind`, `item_option`, `amount`, `chance_bps`, `enabled`)
VALUES (51, 9, 0, 1, 5000, 1)
ON DUPLICATE KEY UPDATE `enabled` = VALUES(`enabled`);

INSERT INTO `mob_drop_list`
  (`mob_species`, `item_kind`, `item_option`, `amount`, `chance_bps`, `enabled`)
VALUES (51, 10, 0, 1, 5000, 1)
ON DUPLICATE KEY UPDATE `enabled` = VALUES(`enabled`);

UPDATE `characters`
SET `pnt_stat` = GREATEST(`pnt_stat`, 20)
WHERE `id` = 16;
COMMIT;

/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
