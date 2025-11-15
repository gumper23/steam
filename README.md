# Steam Library Tracker

A Go application that syncs your Steam game library and playtime data to a MySQL database, with support for generating detailed gaming reports.

## Features

- **Automatic Sync**: Fetches game ownership and playtime from Steam API
- **Historical Tracking**: Records playtime snapshots for activity analysis
- **Gaming Reports**: Generate year-end or custom date range reports
- **Multiple Formats**: Export reports as text, JSON, or Markdown
- **Cron-friendly**: Designed to run every 10 minutes for granular tracking

## Requirements

- Go 1.21+
- MySQL 5.7+
- Steam API key ([get one here](https://steamcommunity.com/dev/apikey))
- Your Steam ID

## Installation

```bash
# Clone the repository
git clone https://github.com/gumper23/steam.git
cd steam

# Install dependencies
go mod download

# Build for current platform (macOS)
go build -o steam main.go
# or
make build

# Build for Linux (cross-compile from macOS)
make build-linux

# Build for both platforms
make build-all
```

## Setup

### 1. Create Configuration File

Create `config.toml` in the application directory:

```toml
[database]
hostname = "localhost"
port = "3306"
username = "your_db_user"
password = "your_db_password"
schema_name = "steam"

[steam]
api_key = "your_steam_api_key"
id = "your_steam_id"
```

### 2. Create Database Tables

Run the migration to create both required tables:

```bash
# Create the games and playtime_snapshots tables
mysql -h localhost -u your_user -p steam < migrations/001_create_playtime_snapshots.sql
```

This creates:
- **games** table - stores your game library
- **playtime_snapshots** table - stores historical playtime data

### 3. Set Up Cron (Optional but Recommended)

For best results, run the sync every 10 minutes:

```bash
# Edit crontab
crontab -e

# Add this line (adjust path as needed)
*/10 * * * * /path/to/steam >> /var/log/steam-sync.log 2>&1
```

## Usage

### Sync Mode (Default)

Fetch latest data from Steam and update the database:

```bash
./steam
```

This will:
1. Fetch your current game library from Steam API
2. Update playtime for existing games
3. Add any new games to the database
4. Record playtime snapshots (for games with 5+ minutes of new playtime)

### Report Mode

Generate gaming reports from historical data:

```bash
# Year-to-date report (default)
./steam --report

# Specific year
./steam --report --start 2024-01-01 --end 2024-12-31

# Last 30 days
./steam --report --start 2024-11-15 --end 2024-12-15

# JSON format (for programmatic use)
./steam --report --format json

# Markdown format (for documentation)
./steam --report --format markdown
```

### Example Report Output

```
=== Gaming Report: 2024-01-01 to 2024-12-15 ===

Total Gaming Time: 12450 minutes (207.5 hours)
Games Played: 47

Top Games by Playtime:
   1. Baldur's Gate 3                        1850 min ( 30.8 hrs)  [Jan 15 - Dec 12]  (145 sessions)
   2. Elden Ring                             1420 min ( 23.7 hrs)  [Feb 03 - Aug 10]  (98 sessions)
   3. Hades                                   980 min ( 16.3 hrs)  [Mar 10 - Nov 25]  (67 sessions)
   ...
```

## Database Schema

### `games` Table
Stores your complete game library:
- Game metadata (name, icons, logos)
- Total playtime across all platforms
- Platform-specific playtime (Windows, Mac, Linux)
- When each game was added to your library

### `playtime_snapshots` Table
Historical playtime tracking:
- Records snapshots every time you play (5+ minute threshold)
- Stores both total playtime and delta since last sync
- Enables time-range queries for reports
- Indexed for efficient date-range lookups

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-cover

# Format code
make fmt

# Run linter
make vet

# Clean up dependencies
make tidy

# Run all checks
make all
```

### Cross-Platform Building

The Makefile supports building for both macOS and Linux:

```bash
# Build for current platform
make build

# Build for Linux AMD64 (from macOS)
make build-linux

# Build both binaries
make build-all

# Output: steam (macOS) and steam-linux (Linux AMD64)
```

Transfer the Linux binary to your Ubuntu server:
```bash
scp steam-linux user@your-server:/path/to/steam
ssh user@your-server 'chmod +x /path/to/steam'
```

## How It Works

1. **Sync Process** (runs every 10 minutes via cron):
   - Fetches current library from Steam API
   - Compares with database to detect changes
   - Updates changed games, inserts new ones
   - Records snapshots for games with 5+ minutes of new playtime

2. **Report Generation**:
   - Queries `playtime_snapshots` for specified date range
   - Aggregates playtime deltas by game
   - Joins with `games` table for metadata
   - Formats output in requested format

3. **Snapshot Logic**:
   - Only records when playtime increases by 5+ minutes
   - Filters out background launches and brief sessions
   - With 10-minute sync intervals, provides good granularity
   - Failures logged but don't stop sync

## Configuration

All configuration fields are required and validated on startup:

**Database Settings:**
- `hostname` - MySQL server host
- `port` - MySQL server port
- `username` - Database user
- `password` - Database password
- `schema_name` - Database name

**Steam Settings:**
- `api_key` - Your Steam Web API key
- `id` - Your Steam ID (numeric)

## Troubleshooting

**"table playtime_snapshots doesn't exist"**
- Run the migration: `mysql ... < migrations/001_create_playtime_snapshots.sql`

**"invalid configuration" error**
- Check that all fields in `config.toml` are filled in
- Ensure no extra spaces or special characters

**No snapshots being recorded**
- Snapshots only record when playtime delta >= 5 minutes
- Check that you're actually playing games between syncs
- Verify the sync is running (check cron logs)

**Report shows no data**
- Snapshots are only recorded going forward (no historical data)
- Play some games, wait for a few sync cycles, then generate report
- Check date range - use `--start` and `--end` to verify

## License

This project is for personal use.

## Acknowledgments

- Steam Web API for game data
- Go community for excellent libraries (BurntSushi/toml, go-sql-driver/mysql)
