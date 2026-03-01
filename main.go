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
	flag "github.com/spf13/pflag"
)

// mysql -h 127.0.0.1 -P13306 steam -BNe "show create table game\G" | grep -o '^[[:blank:]]*`.*`' | sed 's/^[[:blank:]]*//g' | sed 's/`//g' | paste -s -d, - | sed 's/,/\n, /g'

const (
	httpTimeout          = 30 * time.Second
	dbConnectTimeout     = 10 * time.Second
	minPlaytimeThreshold = 5 // Minimum minutes to record a snapshot
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

// PlaytimeSnapshot represents a historical playtime record
type PlaytimeSnapshot struct {
	ID            int
	AppID         int
	PlaytimeTotal int
	PlaytimeDelta int
	SnapshotDate  time.Time
}

// GamePlaySummary holds aggregated play data for a game over a time period
type GamePlaySummary struct {
	AppID         int
	Name          string
	MinutesPlayed int
	HoursPlayed   float64
	FirstPlayed   time.Time
	LastPlayed    time.Time
	SessionCount  int
}

// PlayReport represents a complete gaming report for a time period
type PlayReport struct {
	StartDate    time.Time
	EndDate      time.Time
	AllTime      bool
	TotalMinutes int
	TotalHours   float64
	GamesPlayed  int
	TopGames     []GamePlaySummary
	RecentGames  []GamePlaySummary
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
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC",
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

// getCurrentTimestamp retrieves the current database date for created_at column
func getCurrentTimestamp(ctx context.Context, db *sql.DB) (string, error) {
	var created string
	err := db.QueryRowContext(ctx, "select curdate() as created").Scan(&created)
	if err != nil {
		return "", fmt.Errorf("failed to get current date: %w", err)
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
func insertGame(ctx context.Context, db *sql.DB, game Game) error {
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
	, '1970-01-01'
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
		game.PlaytimeWindowsForever)
	if err != nil {
		return fmt.Errorf("failed to insert game %d: %w", game.Appid, err)
	}
	return nil
}

// getLastSnapshot retrieves the most recent snapshot for a given app_id
func getLastSnapshot(ctx context.Context, db *sql.DB, appid int) (*PlaytimeSnapshot, error) {
	query := `
		SELECT id, app_id, playtime_total, playtime_delta, snapshot_date
		FROM playtime_snapshots
		WHERE app_id = ?
		ORDER BY snapshot_date DESC
		LIMIT 1`

	var snapshot PlaytimeSnapshot
	err := db.QueryRowContext(ctx, query, appid).Scan(
		&snapshot.ID,
		&snapshot.AppID,
		&snapshot.PlaytimeTotal,
		&snapshot.PlaytimeDelta,
		&snapshot.SnapshotDate,
	)

	if err == sql.ErrNoRows {
		return nil, nil // No snapshot exists yet
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get last snapshot for app %d: %w", appid, err)
	}

	return &snapshot, nil
}

// recordPlaytimeSnapshot inserts a new playtime snapshot
func recordPlaytimeSnapshot(ctx context.Context, db *sql.DB, appid, playtimeTotal, playtimeDelta int, snapshotDate time.Time) error {
	query := `
		INSERT INTO playtime_snapshots (app_id, playtime_total, playtime_delta, snapshot_date)
		VALUES (?, ?, ?, ?)`

	_, err := db.ExecContext(ctx, query, appid, playtimeTotal, playtimeDelta, snapshotDate)
	if err != nil {
		return fmt.Errorf("failed to record snapshot for app %d: %w", appid, err)
	}

	return nil
}

// syncGames synchronizes the local database with games from the Steam API
func syncGames(ctx context.Context, db *sql.DB, ogs *OwnedGames, storedGames map[int]int, logger *slog.Logger) (int, int, int, error) {
	var updated, inserted, played int
	snapshotTime := time.Now()

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

				// Record snapshot if delta meets threshold
				delta := game.PlaytimeForever - playtime
				if delta >= minPlaytimeThreshold {
					if err := recordPlaytimeSnapshot(ctx, db, game.Appid, game.PlaytimeForever, delta, snapshotTime); err != nil {
						logger.Warn("failed to record snapshot",
							"app_id", game.Appid,
							"error", err)
					}
				}
			}
		} else {
			if err := insertGame(ctx, db, game); err != nil {
				return updated, inserted, played, err
			}
			inserted++

			// Record initial snapshot if game has playtime and meets threshold
			if game.PlaytimeForever >= minPlaytimeThreshold {
				if err := recordPlaytimeSnapshot(ctx, db, game.Appid, game.PlaytimeForever, game.PlaytimeForever, snapshotTime); err != nil {
					logger.Warn("failed to record initial snapshot",
						"app_id", game.Appid,
						"error", err)
				}
			}
		}
	}

	return updated, inserted, played, nil
}

// getGamesPlayedInRange retrieves all games with activity in the specified date range
func getGamesPlayedInRange(ctx context.Context, db *sql.DB, startDate, endDate time.Time) ([]GamePlaySummary, error) {
	query := `
		SELECT
			s.app_id,
			g.name,
			SUM(s.playtime_delta) as total_minutes,
			MIN(s.snapshot_date) as first_played,
			MAX(s.snapshot_date) as last_played,
			COUNT(*) as session_count
		FROM playtime_snapshots s
		JOIN games g ON s.app_id = g.app_id
		WHERE s.snapshot_date >= ? AND s.snapshot_date <= ?
		GROUP BY s.app_id, g.name
		HAVING total_minutes > 0
		ORDER BY total_minutes DESC`

	rows, err := db.QueryContext(ctx, query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("failed to query games in range: %w", err)
	}
	defer rows.Close()

	var games []GamePlaySummary
	for rows.Next() {
		var game GamePlaySummary
		if err := rows.Scan(
			&game.AppID,
			&game.Name,
			&game.MinutesPlayed,
			&game.FirstPlayed,
			&game.LastPlayed,
			&game.SessionCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan game summary: %w", err)
		}
		game.HoursPlayed = float64(game.MinutesPlayed) / 60.0
		games = append(games, game)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating game summaries: %w", err)
	}

	return games, nil
}

// getGamesAllTime retrieves all games using playtime_forever from the games table,
// which reflects the true all-time total from Steam regardless of when snapshot tracking started.
func getGamesAllTime(ctx context.Context, db *sql.DB) ([]GamePlaySummary, error) {
	query := `
		SELECT
			g.app_id,
			g.name,
			g.playtime_forever as total_minutes,
			MIN(s.snapshot_date) as first_played,
			MAX(s.snapshot_date) as last_played,
			COUNT(s.id) as session_count
		FROM games g
		LEFT JOIN playtime_snapshots s ON g.app_id = s.app_id
		WHERE g.playtime_forever > 0
		GROUP BY g.app_id, g.name
		ORDER BY total_minutes DESC`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all-time games: %w", err)
	}
	defer rows.Close()

	var games []GamePlaySummary
	for rows.Next() {
		var game GamePlaySummary
		var firstPlayed, lastPlayed sql.NullTime
		if err := rows.Scan(
			&game.AppID,
			&game.Name,
			&game.MinutesPlayed,
			&firstPlayed,
			&lastPlayed,
			&game.SessionCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan all-time game summary: %w", err)
		}
		game.HoursPlayed = float64(game.MinutesPlayed) / 60.0
		if firstPlayed.Valid {
			game.FirstPlayed = firstPlayed.Time
		}
		if lastPlayed.Valid {
			game.LastPlayed = lastPlayed.Time
		}
		games = append(games, game)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating all-time game summaries: %w", err)
	}

	return games, nil
}

// getRecentlyPlayedAllTime retrieves the most recently played games across all time,
// using snapshot data for recency ordering.
func getRecentlyPlayedAllTime(ctx context.Context, db *sql.DB, limit int) ([]GamePlaySummary, error) {
	query := `
		SELECT
			s.app_id,
			g.name,
			g.playtime_forever as total_minutes,
			MIN(s.snapshot_date) as first_played,
			MAX(s.snapshot_date) as last_played,
			COUNT(*) as session_count
		FROM playtime_snapshots s
		JOIN games g ON s.app_id = g.app_id
		GROUP BY s.app_id, g.name
		ORDER BY last_played DESC
		LIMIT ?`

	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query all-time recent games: %w", err)
	}
	defer rows.Close()

	var games []GamePlaySummary
	for rows.Next() {
		var game GamePlaySummary
		if err := rows.Scan(
			&game.AppID,
			&game.Name,
			&game.MinutesPlayed,
			&game.FirstPlayed,
			&game.LastPlayed,
			&game.SessionCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan all-time recent game: %w", err)
		}
		game.HoursPlayed = float64(game.MinutesPlayed) / 60.0
		games = append(games, game)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating all-time recent games: %w", err)
	}

	return games, nil
}

// getRecentlyPlayedGames retrieves the most recently played games (by last_played date)
func getRecentlyPlayedGames(ctx context.Context, db *sql.DB, startDate, endDate time.Time, limit int) ([]GamePlaySummary, error) {
	query := `
		SELECT
			s.app_id,
			g.name,
			SUM(s.playtime_delta) as total_minutes,
			MIN(s.snapshot_date) as first_played,
			MAX(s.snapshot_date) as last_played,
			COUNT(*) as session_count
		FROM playtime_snapshots s
		JOIN games g ON s.app_id = g.app_id
		WHERE s.snapshot_date >= ? AND s.snapshot_date <= ?
		GROUP BY s.app_id, g.name
		HAVING total_minutes > 0
		ORDER BY last_played DESC
		LIMIT ?`

	rows, err := db.QueryContext(ctx, query, startDate, endDate, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query recent games: %w", err)
	}
	defer rows.Close()

	var games []GamePlaySummary
	for rows.Next() {
		var game GamePlaySummary
		if err := rows.Scan(
			&game.AppID,
			&game.Name,
			&game.MinutesPlayed,
			&game.FirstPlayed,
			&game.LastPlayed,
			&game.SessionCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan recent game: %w", err)
		}
		game.HoursPlayed = float64(game.MinutesPlayed) / 60.0
		games = append(games, game)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating recent games: %w", err)
	}

	return games, nil
}

// generatePlayReport creates a complete play report for the given time period.
// When allTime is true, playtime totals come from games.playtime_forever (the authoritative
// Steam value) rather than summing snapshot deltas, so games tracked before the snapshot
// system started are correctly ranked.
func generatePlayReport(ctx context.Context, db *sql.DB, startDate, endDate time.Time, allTime bool) (*PlayReport, error) {
	var games, recentGames []GamePlaySummary
	var err error

	if allTime {
		games, err = getGamesAllTime(ctx, db)
		if err != nil {
			return nil, err
		}
		recentGames, err = getRecentlyPlayedAllTime(ctx, db, 5)
		if err != nil {
			return nil, err
		}
	} else {
		games, err = getGamesPlayedInRange(ctx, db, startDate, endDate)
		if err != nil {
			return nil, err
		}
		recentGames, err = getRecentlyPlayedGames(ctx, db, startDate, endDate, 5)
		if err != nil {
			return nil, err
		}
	}

	totalMinutes := 0
	for _, game := range games {
		totalMinutes += game.MinutesPlayed
	}

	report := &PlayReport{
		StartDate:    startDate,
		EndDate:      endDate,
		TotalMinutes: totalMinutes,
		TotalHours:   float64(totalMinutes) / 60.0,
		GamesPlayed:  len(games),
		TopGames:     games,       // Sorted by playtime DESC
		RecentGames:  recentGames, // Sorted by last_played DESC
	}

	return report, nil
}

// formatReportText formats a report as human-readable text
func formatReportText(report *PlayReport) string {
	var output string
	if report.AllTime {
		output += "=== Gaming Report: All Time ===\n\n"
	} else {
		output += fmt.Sprintf("=== Gaming Report: %s to %s ===\n\n",
			report.StartDate.Format("2006-01-02"),
			report.EndDate.Format("2006-01-02"))
	}

	output += fmt.Sprintf("Total Gaming Time: %d minutes (%.1f hours)\n", report.TotalMinutes, report.TotalHours)
	output += fmt.Sprintf("Games Played: %d\n\n", report.GamesPlayed)

	// Show recently played games first
	if len(report.RecentGames) > 0 {
		output += "Recently Played (Last 5):\n"
		for i, game := range report.RecentGames {
			output += fmt.Sprintf("  %d. %-40s %5d min (%5.1f hrs)  Last: %s\n",
				i+1,
				truncateString(game.Name, 40),
				game.MinutesPlayed,
				game.HoursPlayed,
				game.LastPlayed.Local().Format("Jan 02 15:04"))
		}
		output += "\n"
	}

	if len(report.TopGames) > 0 {
		output += "Top Games by Playtime:\n"
		for i, game := range report.TopGames {
			if i >= 20 { // Show top 20
				break
			}
			if report.AllTime {
				output += fmt.Sprintf("  %2d. %-40s %5d min (%5.1f hrs)\n",
					i+1,
					truncateString(game.Name, 40),
					game.MinutesPlayed,
					game.HoursPlayed)
			} else {
				output += fmt.Sprintf("  %2d. %-40s %5d min (%5.1f hrs)  [%s - %s]  (%d sessions)\n",
					i+1,
					truncateString(game.Name, 40),
					game.MinutesPlayed,
					game.HoursPlayed,
					game.FirstPlayed.Local().Format("Jan 02"),
					game.LastPlayed.Local().Format("Jan 02"),
					game.SessionCount)
			}
		}
	} else {
		output += "No games played in this period.\n"
	}

	return output
}

// formatReportJSON formats a report as JSON
func formatReportJSON(report *PlayReport) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal report to JSON: %w", err)
	}
	return string(data), nil
}

// formatReportMarkdown formats a report as Markdown
func formatReportMarkdown(report *PlayReport) string {
	var output string
	if report.AllTime {
		output += "# Gaming Report: All Time\n\n"
	} else {
		output += fmt.Sprintf("# Gaming Report: %s to %s\n\n",
			report.StartDate.Format("January 2, 2006"),
			report.EndDate.Format("January 2, 2006"))
	}

	output += fmt.Sprintf("**Total Gaming Time:** %.1f hours (%d minutes)\n\n", report.TotalHours, report.TotalMinutes)
	output += fmt.Sprintf("**Games Played:** %d\n\n", report.GamesPlayed)

	if len(report.RecentGames) > 0 {
		output += "## Recently Played (Last 5)\n\n"
		output += "| Game | Time Played | Last Played |\n"
		output += "|------|-------------|-------------|\n"

		for _, game := range report.RecentGames {
			output += fmt.Sprintf("| %s | %.1f hrs | %s |\n",
				game.Name,
				game.HoursPlayed,
				game.LastPlayed.Local().Format("Jan 02 15:04"))
		}
		output += "\n"
	}

	if len(report.TopGames) > 0 {
		output += "## Top Games\n\n"
		if report.AllTime {
			output += "| Rank | Game | Time Played |\n"
			output += "|------|------|-------------|\n"
			for i, game := range report.TopGames {
				if i >= 20 { // Show top 20
					break
				}
				output += fmt.Sprintf("| %d | %s | %.1f hrs |\n",
					i+1,
					game.Name,
					game.HoursPlayed)
			}
		} else {
			output += "| Rank | Game | Time Played | Sessions | Period |\n"
			output += "|------|------|-------------|----------|--------|\n"
			for i, game := range report.TopGames {
				if i >= 20 { // Show top 20
					break
				}
				output += fmt.Sprintf("| %d | %s | %.1f hrs | %d | %s - %s |\n",
					i+1,
					game.Name,
					game.HoursPlayed,
					game.SessionCount,
					game.FirstPlayed.Local().Format("Jan 02"),
					game.LastPlayed.Local().Format("Jan 02"))
			}
		}
	} else {
		output += "No games played in this period.\n"
	}

	return output
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// runReport generates and displays a gaming report
func runReport(ctx context.Context, db *sql.DB, startDate, endDate time.Time, format string, allTime bool, logger *slog.Logger) error {
	if allTime {
		logger.Info("generating report", "period", "all time", "format", format)
	} else {
		logger.Info("generating report",
			"start_date", startDate.Format("2006-01-02"),
			"end_date", endDate.Format("2006-01-02"),
			"format", format)
	}

	report, err := generatePlayReport(ctx, db, startDate, endDate, allTime)
	if err != nil {
		return err
	}
	report.AllTime = allTime

	var output string
	switch format {
	case "json":
		output, err = formatReportJSON(report)
		if err != nil {
			return err
		}
	case "markdown", "md":
		output = formatReportMarkdown(report)
	default: // "text" or anything else
		output = formatReportText(report)
	}

	fmt.Println(output)
	return nil
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

	updated, inserted, played, err := syncGames(ctx, db, ogs, storedGames, logger)
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
	// Parse command-line flags
	reportMode := flag.BoolP("report", "r", false, "Generate a gaming report instead of syncing")
	startDateStr := flag.StringP("start", "s", "", "Report start date (YYYY-MM-DD)")
	endDateStr := flag.StringP("end", "e", "", "Report end date (YYYY-MM-DD)")
	yearToDate := flag.BoolP("ytd", "y", false, "Year-to-date report (Jan 1 to now)")
	lastWeek := flag.BoolP("last-week", "w", false, "Report for last 7 days")
	lastMonth := flag.BoolP("last-month", "m", false, "Report for last 30 days")
	lastYear := flag.BoolP("last-year", "l", false, "Report for last 365 days")
	allTime := flag.BoolP("all-time", "a", false, "Report for all time")
	reportFormat := flag.StringP("format", "f", "text", "Report format: text, json, or markdown")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Report mode
	if *reportMode {
		config, err := loadConfig("config.toml")
		if err != nil {
			logger.Error("failed to load config", "error", err)
			os.Exit(1)
		}

		db, err := connectDB(ctx, config.Database.DSN())
		if err != nil {
			logger.Error("failed to connect to database", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		// Determine date range
		var startDate, endDate time.Time
		now := time.Now()

		// Check how many date range options are set
		optionsSet := 0
		if *startDateStr != "" || *endDateStr != "" {
			optionsSet++
		}
		if *yearToDate {
			optionsSet++
		}
		if *lastWeek {
			optionsSet++
		}
		if *lastMonth {
			optionsSet++
		}
		if *lastYear {
			optionsSet++
		}
		if *allTime {
			optionsSet++
		}

		if optionsSet > 1 {
			logger.Error("cannot specify multiple date range options (--ytd, --last-week, --last-month, --last-year, --all-time, or --start/--end)")
			os.Exit(1)
		}

		if *startDateStr != "" && *endDateStr != "" {
			// Custom date range - parse in local time so dates match the user's calendar
			startDate, err = time.ParseInLocation("2006-01-02", *startDateStr, now.Location())
			if err != nil {
				logger.Error("invalid start date", "error", err)
				os.Exit(1)
			}
			endDate, err = time.ParseInLocation("2006-01-02", *endDateStr, now.Location())
			if err != nil {
				logger.Error("invalid end date", "error", err)
				os.Exit(1)
			}
			// Set end date to end of day
			endDate = endDate.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
		} else if *startDateStr != "" || *endDateStr != "" {
			logger.Error("must specify both --start and --end dates")
			os.Exit(1)
		} else if *lastWeek {
			// Last 7 days
			startDate = now.AddDate(0, 0, -7)
			endDate = now
		} else if *lastMonth {
			// Last 30 days
			startDate = now.AddDate(0, 0, -30)
			endDate = now
		} else if *lastYear {
			// Last 365 days
			startDate = now.AddDate(0, 0, -365)
			endDate = now
		} else if *allTime {
			// All time: use epoch as start
			startDate = time.Date(1970, 1, 1, 0, 0, 0, 0, now.Location())
			endDate = now
		} else {
			// Default to year-to-date if no option specified
			startDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
			endDate = now
		}

		if err := runReport(ctx, db, startDate, endDate, *reportFormat, *allTime, logger); err != nil {
			logger.Error("report generation failed", "error", err)
			os.Exit(1)
		}

		logger.Info("report generation complete")
		return
	}

	// Sync mode (default)
	if err := run(ctx, logger); err != nil {
		logger.Error("execution failed", "error", err)
		os.Exit(1)
	}

	logger.Info("execution complete")
}
