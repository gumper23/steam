-- Migration: Create playtime_snapshots table
-- This table tracks historical playtime data for generating reports
-- Run this migration before using the report functionality

CREATE TABLE IF NOT EXISTS playtime_snapshots (
    id INT UNSIGNED NOT NULL AUTO_INCREMENT,
    app_id INT UNSIGNED NOT NULL,
    playtime_total INT UNSIGNED NOT NULL COMMENT 'Total playtime at this snapshot (minutes)',
    playtime_delta INT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'Minutes played since last snapshot',
    snapshot_date DATETIME NOT NULL COMMENT 'When this snapshot was recorded',
    PRIMARY KEY (id),
    KEY idx_app_id_date (app_id, snapshot_date),
    KEY idx_snapshot_date (snapshot_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
COMMENT='Historical playtime snapshots for reporting';
