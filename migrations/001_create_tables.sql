-- Migration: Create games and playtime_snapshots tables
-- This migration creates the core database schema for the Steam library tracker
-- Run this migration to set up the database

-- Create games table first
create table if not exists games (
    id int unsigned not null auto_increment,
    app_id int unsigned not null,
    has_community_visible_stats tinyint unsigned not null,
    img_icon_url varchar(255) character set utf8mb4 collate utf8mb4_unicode_ci not null,
    img_logo_url varchar(255) character set utf8mb4 collate utf8mb4_unicode_ci not null,
    name varchar(255) character set utf8mb4 collate utf8mb4_unicode_ci not null,
    playtime_2weeks int unsigned not null default 0,
    playtime_forever int unsigned not null default 0,
    playtime_linux_forever int unsigned not null default 0,
    playtime_mac_forever int unsigned not null default 0,
    playtime_windows_forever int unsigned not null default 0,
    created_at date not null,
    primary key (id),
    unique key app_id (app_id),
    key name (name(20)),
    key playtime_forever (playtime_forever),
    key created_at (created_at)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_unicode_ci;

-- Create playtime_snapshots table for historical tracking
create table if not exists playtime_snapshots (
    id int unsigned not null auto_increment,
    app_id int unsigned not null,
    playtime_total int unsigned not null comment 'Total playtime at this snapshot (minutes)',
    playtime_delta int unsigned not null default 0 comment 'Minutes played since last snapshot',
    snapshot_date datetime not null comment 'When this snapshot was recorded',
    primary key (id),
    key idx_app_id_date (app_id, snapshot_date),
    key idx_snapshot_date (snapshot_date)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_unicode_ci
comment='Historical playtime snapshots for reporting';
