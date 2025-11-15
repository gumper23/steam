# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Steam library tracking application that syncs game ownership and playtime data from the Steam API to a MySQL database. The application is written in Go and follows modern Go conventions (Go 1.21+).

## Architecture

**Single Binary Design:**
- All code is in `main.go` (with tests in `main_test.go`)
- The application runs as a CLI tool that performs one-time sync operations
- No server/daemon component - designed to be run periodically (e.g., via cron)

**Data Flow:**
1. Load configuration from `config.toml`
2. Connect to MySQL database
3. Fetch current game library from Steam API
4. Load existing games from database into memory (map of app_id -> playtime)
5. Compare and sync: update playtime for existing games, insert new games
6. Log summary statistics

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
```

**Run tests:**
```bash
go test -v -cover
```

**Run tests with race detection:**
```bash
go test -v -race -cover
```

**Run specific test:**
```bash
go test -v -run TestConfig_Validate
go test -v -run TestSyncGames
```

**Coverage report:**
```bash
go test -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out  # for HTML view
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

The application works with a `games` table in MySQL. See the comment block at the top of `main.go` for the full schema definition. Key columns:
- `app_id` (unique) - Steam application ID
- `playtime_forever` - Total minutes played (this is what gets updated)
- `playtime_2weeks`, `playtime_linux_forever`, etc. - Platform-specific playtime
- `created_at` - When the game was first added to the database

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
