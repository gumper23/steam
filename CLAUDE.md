# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Steam library tracking application that syncs game ownership and playtime data from the Steam API to a MySQL database, and generates gaming reports. The application is written in Go and follows modern Go conventions (Go 1.21+).

**Dual-mode operation:**
- **Sync mode** (default): Fetches data from Steam API and updates the database
- **Report mode**: Generates gaming reports from historical playtime data

## Architecture

**Single Binary Design:**
- All code is in `main.go` (with tests in `main_test.go`)
- The application runs as a CLI tool that performs one-time sync operations
- No server/daemon component - designed to be run periodically (e.g., via cron)

**Data Flow (Sync Mode):**
1. Load configuration from `config.toml`
2. Connect to MySQL database
3. Fetch current game library from Steam API
4. Load existing games from database into memory (map of app_id -> playtime)
5. Compare and sync: update playtime for existing games, insert new games
6. Record playtime snapshots for games with 5+ minutes of new playtime
7. Log summary statistics

**Data Flow (Report Mode):**
1. Load configuration and connect to database
2. Parse date range (year-to-date by default, or custom range)
3. Query `playtime_snapshots` table for activity in date range
4. Aggregate playtime deltas by game
5. Format and output report (text, JSON, or Markdown)

**Key Design Patterns:**
- Context-based operations throughout (all DB/HTTP calls accept `context.Context`)
- Structured logging with `log/slog`
- Parameterized SQL queries to prevent injection
- Error wrapping with `fmt.Errorf` and `%w` for error chains
- Table-driven tests using `sqlmock` for database operations

## Development Commands

**Build:**
```bash
go build -o steam main.go
# Or use Make
make build
```

**Run in sync mode (default):**
```bash
./steam
```

**Run in report mode:**
```bash
# Year-to-date report (default)
./steam --report

# Custom date range
./steam --report --start 2024-01-01 --end 2024-12-31

# Different formats
./steam --report --format json
./steam --report --format markdown
./steam --report --format text
```

**Run tests:**
```bash
go test -v -cover
# Or use Make
make test
make test-verbose
make test-race
make test-cover
```

**Run specific test:**
```bash
go test -v -run TestConfig_Validate
go test -v -run TestSyncGames
```

**Other Make targets:**
```bash
make tidy     # Run go mod tidy
make fmt      # Format code
make vet      # Run go vet
make all      # Run fmt, vet, tidy, and test
```

**Install dependencies:**
```bash
go mod download
```

## Configuration

The application expects a `config.toml` file in the working directory:

```toml
[database]
hostname = "localhost"
port = "3306"
username = "user"
password = "pass"
schema_name = "steam"

[steam]
api_key = "your-steam-api-key"
id = "your-steam-id"
```

All configuration fields are required and validated at startup via `Config.Validate()`.

## Database Schema

**Setup:**
Before using the report functionality, run the migration:
```bash
mysql -h <host> -u <user> -p <database> < migrations/001_create_playtime_snapshots.sql
```

**Tables:**

1. **`games`** - Main table storing game library
   - `app_id` (unique) - Steam application ID
   - `playtime_forever` - Total minutes played (gets updated on each sync)
   - `playtime_2weeks`, `playtime_linux_forever`, etc. - Platform-specific playtime
   - `name`, `img_icon_url`, `img_logo_url` - Game metadata
   - `created_at` - When the game was first added to the database

2. **`playtime_snapshots`** - Historical playtime tracking (for reports)
   - `app_id` - Links to games table (no FK constraint by design)
   - `playtime_total` - Total playtime at this snapshot
   - `playtime_delta` - Minutes played since last snapshot
   - `snapshot_date` - When this snapshot was recorded
   - Indexes on `(app_id, snapshot_date)` and `snapshot_date` for efficient queries
   - Snapshots are only recorded when playtime delta >= 5 minutes

See `main.go` (top comment) for full `games` schema and `migrations/001_create_playtime_snapshots.sql` for snapshots table.

## Testing Strategy

**Mock-based testing:**
- Use `github.com/DATA-DOG/go-sqlmock` for database mocking
- Each database function (`getStoredGames`, `updateGame`, `insertGame`) has isolated tests
- Configuration and validation logic uses table-driven tests

**What's tested:**
- Configuration validation (all required fields)
- DSN string building
- SQL operations (parameterized queries)
- Error handling paths
- Business logic (sync decisions: insert vs update)

**What's NOT tested (by design):**
- `connectDB()`, `run()`, `main()` - integration functions requiring real DB/network
- Actual Steam API integration - `fetchOwnedGames` tests only validate HTTP handling logic

**When adding new functions:**
- Extract business logic into separate functions that accept `context.Context` and `*sql.DB`
- Write tests using `sqlmock` to verify SQL queries and parameter binding
- Use table-driven tests for validation or parsing logic

## Important Implementation Notes

**Security:**
- All SQL queries use parameterized statements (never string concatenation)
- Steam API parameters are URL-escaped via `url.QueryEscape()`
- Configuration validation prevents empty required fields

**Timeouts:**
- HTTP client: 30 seconds (see `httpTimeout` constant)
- Overall execution: 5 minutes (see `main()` context timeout)
- Database connection timeout: 10 seconds constant defined but not currently used

**Error Handling:**
- All errors are wrapped with context using `fmt.Errorf("description: %w", err)`
- Main execution function returns errors to `main()` rather than calling `os.Exit()` directly
- Only `main()` calls `os.Exit(1)` on failure

**Logging:**
- Use structured logging: `logger.Info("message", "key", value, ...)`
- Logger is passed as parameter to functions that need it (e.g., `syncGames`)
- Avoid logging in library-style functions; return errors instead

**Playtime Snapshot Logic:**
- Snapshots are recorded during sync when playtime delta >= 5 minutes (see `minPlaytimeThreshold`)
- This filters out games that launch briefly or run in background
- Application is designed to run every 10 minutes via cron for good granularity
- Snapshot failures are logged as warnings but don't fail the sync
- Initial snapshots for new games use total playtime as the delta

**Report Generation:**
- Default mode is year-to-date (Jan 1 to current date)
- Custom ranges supported via `--start` and `--end` flags
- Reports show top 20 games by playtime
- Three output formats: text (default), JSON, and Markdown
- Queries use `SUM(playtime_delta)` to calculate total play time in range
- Games are sorted by total playtime descending
