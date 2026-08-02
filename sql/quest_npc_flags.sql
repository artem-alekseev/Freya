-- Apply once to an existing World DB created before quest stage persistence.
ALTER TABLE `characters_quests`
    ADD COLUMN `npc_flags` SMALLINT UNSIGNED NOT NULL DEFAULT '0' AFTER `expand`;
