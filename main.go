package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/go-sql-driver/mysql"
)

/*
mysql -h 127.0.0.1 -P13306 steam -BNe "show create table games\G" | tr '[:upper:]' '[:lower:]' | sed 1,2d | sed 's/`//g' | sed 's/^  /    /g' | sed 's/auto_increment=[0-9]* //g'

create table if not exists games (
    id int(10) unsigned not null auto_increment,
    app_id int(10) unsigned not null,
    has_community_visible_stats tinyint(3) unsigned not null,
    img_icon_url varchar(255) character set utf8mb4 collate utf8mb4_unicode_ci not null,
    img_logo_url varchar(255) character set utf8mb4 collate utf8mb4_unicode_ci not null,
    name varchar(255) character set utf8mb4 collate utf8mb4_unicode_ci not null,
    playtime_2weeks int(10) unsigned not null default '0',
    playtime_forever int(10) unsigned not null default '0',
    playtime_linux_forever int(10) unsigned not null default '0',
    playtime_mac_forever int(10) unsigned not null default '0',
    playtime_windows_forever int(10) unsigned not null default '0',
    created_at date not null,
    primary key (id),
    unique key app_id (app_id),
    key name (name(20)),
    key playtime_forever (playtime_forever),
    key created_at (created_at)
) engine=innodb default charset=utf8mb4 collate=utf8mb4_unicode_ci;

insert into games(id, app_id, has_community_visible_stats, img_icon_url, img_logo_url, name, playtime_forever, created_at)
select id, app_id, has_community_visible_stats, img_icon_url, img_logo_url, name, playtime_forever, created_at from game g;

insert into games(id, app_id, has_community_visible_stats, img_icon_url, img_logo_url, name, playtime_forever, created_at)
select id, app_id, has_community_visible_stats, img_icon_url, img_logo_url, name, playtime_forever, created_at from game g
where not exists (select 1 from games gs where g.app_id = gs.app_id)
*/

// mysql -h 127.0.0.1 -P13306 steam -BNe "show create table game\G" | grep -o '^[[:blank:]]*`.*`' | sed 's/^[[:blank:]]*//g' | sed 's/`//g' | paste -s -d, - | sed 's/,/, /g'
// mysql -h 127.0.0.1 -P13306 steam -BNe "show create table game\G" | grep -o '^[[:blank:]]*`.*`' | sed 's/^[[:blank:]]*//g' | sed 's/`//g' | paste -s -d, - | sed 's/,/\n, /g'

const (
	httpTimeout      = 30 * time.Second
	dbConnectTimeout = 10 * time.Second
)

// Game holds a steam owned game api game
type Game struct {
	Appid                    int    `json:"appid"`
	HasCommunityVisibleStats bool   `json:"has_community_visible_stats"`
	ImgIconURL               string `json:"img_icon_url"`
	ImgLogoURL               string `json:"img_logo_url"`
	Name                     string `json:"name"`
	Playtime2weeks           int    `json:"playtime_2weeks"`
	PlaytimeForever          int    `json:"playtime_forever"`
	PlaytimeLinuxForever     int    `json:"playtime_linux_forever"`
	PlaytimeMacForever       int    `json:"playtime_mac_forever"`
	PlaytimeWindowsForever   int    `json:"playtime_windows_forever"`
}

// OwnedGames holds all steam games from the owned games API
type OwnedGames struct {
	Response struct {
		GameCount int    `json:"game_count"`
		Games     []Game `json:"games"`
	} `json:"response"`
}

// Steam holds credentials for accessing the Steam API
type Steam struct {
	APIKey string `toml:"api_key"`
	ID     string `toml:"id"`
}

// Database holds credentials for accessing the database
type Database struct {
	Host     string `toml:"hostname"`
	Port     string `toml:"port"`
	User     string `toml:"username"`
	Password string `toml:"password"`
	Schema   string `toml:"schema_name"`
}

// Config holds both database and steam credentials
type Config struct {
	Database Database `toml:"database"`
	Steam    Steam    `toml:"steam"`
}

// StoredGame holds the number of minutes played by app id.
type StoredGame struct {
	Appid           int
	PlaytimeForever int
}

// Validate checks that all required configuration fields are populated
func (c *Config) Validate() error {
	if c.Database.Host == "" {
		return errors.New("database hostname is required")
	}
	if c.Database.Port == "" {
		return errors.New("database port is required")
	}
	if c.Database.User == "" {
		return errors.New("database username is required")
	}
	if c.Database.Schema == "" {
		return errors.New("database schema_name is required")
	}
	if c.Steam.APIKey == "" {
		return errors.New("steam api_key is required")
	}
	if c.Steam.ID == "" {
		return errors.New("steam id is required")
	}
	return nil
}

// DSN returns a MySQL data source name from the database configuration
func (d *Database) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
		d.User,
		d.Password,
		d.Host,
		d.Port,
		d.Schema)
}

// loadConfig reads and validates the configuration file
func loadConfig(filename string) (*Config, error) {
	var config Config
	if _, err := toml.DecodeFile(filename, &config); err != nil {
		return nil, fmt.Errorf("failed to decode config file: %w", err)
	}
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return &config, nil
}

// connectDB establishes a database connection and verifies it with a ping
func connectDB(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// getCurrentTimestamp retrieves the current database timestamp
func getCurrentTimestamp(ctx context.Context, db *sql.DB) (string, error) {
	var created string
	err := db.QueryRowContext(ctx, "select current_timestamp() as created").Scan(&created)
	if err != nil {
		return "", fmt.Errorf("failed to get current timestamp: %w", err)
	}
	return created, nil
}

// getStoredGames retrieves all games from the database as a map of appid to playtime
func getStoredGames(ctx context.Context, db *sql.DB) (map[int]int, error) {
	rows, err := db.QueryContext(ctx, "select app_id, playtime_forever from games")
	if err != nil {
		return nil, fmt.Errorf("failed to query stored games: %w", err)
	}
	defer rows.Close()

	sgs := make(map[int]int)
	for rows.Next() {
		var sg StoredGame
		if err := rows.Scan(&sg.Appid, &sg.PlaytimeForever); err != nil {
			return nil, fmt.Errorf("failed to scan stored game: %w", err)
		}
		sgs[sg.Appid] = sg.PlaytimeForever
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating stored games: %w", err)
	}

	return sgs, nil
}

// fetchOwnedGames retrieves the list of owned games from the Steam API
func fetchOwnedGames(ctx context.Context, client *http.Client, apiKey, steamID string) (*OwnedGames, error) {
	steamURL := fmt.Sprintf("https://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/?key=%s&steamid=%s&include_appinfo=1&format=json",
		url.QueryEscape(apiKey),
		url.QueryEscape(steamID))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, steamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch owned games: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("steam API returned status %d", res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var ogs OwnedGames
	if err := json.Unmarshal(body, &ogs); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &ogs, nil
}

// updateGame updates the playtime for an existing game
func updateGame(ctx context.Context, db *sql.DB, appid, playtime int) error {
	_, err := db.ExecContext(ctx,
		"update games set playtime_forever = ? where app_id = ?",
		playtime, appid)
	if err != nil {
		return fmt.Errorf("failed to update game %d: %w", appid, err)
	}
	return nil
}

// insertGame inserts a new game into the database
func insertGame(ctx context.Context, db *sql.DB, game Game, created string) error {
	query := `
insert into games
(
	app_id
	, has_community_visible_stats
	, img_icon_url
	, img_logo_url
	, name
	, playtime_2weeks
	, playtime_forever
	, playtime_linux_forever
	, playtime_mac_forever
	, playtime_windows_forever
	, created_at
)
values
(
    ?
    , ?
    , ?
    , ?
    , ?
    , ?
	, ?
	, ?
	, ?
	, ?
	, ?
)`
	_, err := db.ExecContext(ctx, query,
		game.Appid,
		game.HasCommunityVisibleStats,
		game.ImgIconURL,
		game.ImgLogoURL,
		game.Name,
		game.Playtime2weeks,
		game.PlaytimeForever,
		game.PlaytimeLinuxForever,
		game.PlaytimeMacForever,
		game.PlaytimeWindowsForever,
		created)
	if err != nil {
		return fmt.Errorf("failed to insert game %d: %w", game.Appid, err)
	}
	return nil
}

// syncGames synchronizes the local database with games from the Steam API
func syncGames(ctx context.Context, db *sql.DB, ogs *OwnedGames, storedGames map[int]int, created string, logger *slog.Logger) (int, int, int, error) {
	var updated, inserted, played int

	for _, game := range ogs.Response.Games {
		if game.PlaytimeForever != 0 {
			played++
		}

		if playtime, ok := storedGames[game.Appid]; ok {
			if playtime != game.PlaytimeForever {
				if err := updateGame(ctx, db, game.Appid, game.PlaytimeForever); err != nil {
					return updated, inserted, played, err
				}
				updated++
			}
		} else {
			if err := insertGame(ctx, db, game, created); err != nil {
				return updated, inserted, played, err
			}
			inserted++
		}
	}

	return updated, inserted, played, nil
}

func run(ctx context.Context, logger *slog.Logger) error {
	logger.Info("beginning execution")

	config, err := loadConfig("config.toml")
	if err != nil {
		return err
	}

	db, err := connectDB(ctx, config.Database.DSN())
	if err != nil {
		return err
	}
	defer db.Close()

	created, err := getCurrentTimestamp(ctx, db)
	if err != nil {
		return err
	}

	storedGames, err := getStoredGames(ctx, db)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: httpTimeout,
	}

	ogs, err := fetchOwnedGames(ctx, client, config.Steam.APIKey, config.Steam.ID)
	if err != nil {
		return err
	}

	updated, inserted, played, err := syncGames(ctx, db, ogs, storedGames, created, logger)
	if err != nil {
		return err
	}

	total := len(ogs.Response.Games)
	playedPercent := 0.0
	if total > 0 {
		playedPercent = float64(played) / float64(total) * 100.0
	}

	logger.Info("sync complete",
		"total_games", total,
		"played_games", played,
		"new_games", inserted,
		"updated_games", updated,
		"played_percent", fmt.Sprintf("%.2f", playedPercent))

	return nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := run(ctx, logger); err != nil {
		logger.Error("execution failed", "error", err)
		os.Exit(1)
	}

	logger.Info("execution complete")
}
