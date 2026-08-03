-- Apply once to an existing World DB.
ALTER TABLE `characters`
    ADD COLUMN `premium_service` TINYINT UNSIGNED NOT NULL DEFAULT '0' AFTER `war_exp`,
    ADD COLUMN `premium_expire` BIGINT UNSIGNED NOT NULL DEFAULT '0' AFTER `premium_service`;
