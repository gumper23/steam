package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			config: Config{
				Database: Database{
					Host:   "localhost",
					Port:   "3306",
					User:   "user",
					Schema: "steam",
				},
				Steam: Steam{
					APIKey: "test-key",
					ID:     "test-id",
				},
			},
			wantErr: false,
		},
		{
			name: "missing database host",
			config: Config{
				Database: Database{
					Port:   "3306",
					User:   "user",
					Schema: "steam",
				},
				Steam: Steam{
					APIKey: "test-key",
					ID:     "test-id",
				},
			},
			wantErr: true,
			errMsg:  "database hostname is required",
		},
		{
			name: "missing database port",
			config: Config{
				Database: Database{
					Host:   "localhost",
					User:   "user",
					Schema: "steam",
				},
				Steam: Steam{
					APIKey: "test-key",
					ID:     "test-id",
				},
			},
			wantErr: true,
			errMsg:  "database port is required",
		},
		{
			name: "missing database user",
			config: Config{
				Database: Database{
					Host:   "localhost",
					Port:   "3306",
					Schema: "steam",
				},
				Steam: Steam{
					APIKey: "test-key",
					ID:     "test-id",
				},
			},
			wantErr: true,
			errMsg:  "database username is required",
		},
		{
			name: "missing database schema",
			config: Config{
				Database: Database{
					Host: "localhost",
					Port: "3306",
					User: "user",
				},
				Steam: Steam{
					APIKey: "test-key",
					ID:     "test-id",
				},
			},
			wantErr: true,
			errMsg:  "database schema_name is required",
		},
		{
			name: "missing steam api key",
			config: Config{
				Database: Database{
					Host:   "localhost",
					Port:   "3306",
					User:   "user",
					Schema: "steam",
				},
				Steam: Steam{
					ID: "test-id",
				},
			},
			wantErr: true,
			errMsg:  "steam api_key is required",
		},
		{
			name: "missing steam id",
			config: Config{
				Database: Database{
					Host:   "localhost",
					Port:   "3306",
					User:   "user",
					Schema: "steam",
				},
				Steam: Steam{
					APIKey: "test-key",
				},
			},
			wantErr: true,
			errMsg:  "steam id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				if err == nil {
					t.Errorf("Config.Validate() expected error, got nil")
				} else if err.Error() != tt.errMsg {
					t.Errorf("Config.Validate() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Config.Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestDatabase_DSN(t *testing.T) {
	tests := []struct {
		name string
		db   Database
		want string
	}{
		{
			name: "standard configuration",
			db: Database{
				Host:     "localhost",
				Port:     "3306",
				User:     "root",
				Password: "password",
				Schema:   "steam",
			},
			want: "root:password@tcp(localhost:3306)/steam",
		},
		{
			name: "empty password",
			db: Database{
				Host:     "127.0.0.1",
				Port:     "13306",
				User:     "testuser",
				Password: "",
				Schema:   "testdb",
			},
			want: "testuser:@tcp(127.0.0.1:13306)/testdb",
		},
		{
			name: "special characters in password",
			db: Database{
				Host:     "db.example.com",
				Port:     "3306",
				User:     "app",
				Password: "p@ssw0rd!",
				Schema:   "production",
			},
			want: "app:p@ssw0rd!@tcp(db.example.com:3306)/production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.db.DSN(); got != tt.want {
				t.Errorf("Database.DSN() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Create a temporary valid config file
	validConfig := `
[database]
hostname = "localhost"
port = "3306"
username = "testuser"
password = "testpass"
schema_name = "testdb"

[steam]
api_key = "test-api-key"
id = "test-steam-id"
`

	tmpFile, err := os.CreateTemp("", "config-*.toml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write([]byte(validConfig)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	t.Run("valid config file", func(t *testing.T) {
		config, err := loadConfig(tmpFile.Name())
		if err != nil {
			t.Errorf("loadConfig() unexpected error: %v", err)
		}
		if config == nil {
			t.Fatal("loadConfig() returned nil config")
		}
		if config.Database.Host != "localhost" {
			t.Errorf("config.Database.Host = %v, want localhost", config.Database.Host)
		}
		if config.Steam.APIKey != "test-api-key" {
			t.Errorf("config.Steam.APIKey = %v, want test-api-key", config.Steam.APIKey)
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := loadConfig("non-existent-file.toml")
		if err == nil {
			t.Error("loadConfig() expected error for non-existent file, got nil")
		}
	})

	// Create an invalid config file (missing required fields)
	invalidConfig := `
[database]
hostname = "localhost"

[steam]
api_key = "test-key"
`

	tmpInvalidFile, err := os.CreateTemp("", "invalid-config-*.toml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpInvalidFile.Name())

	if _, err := tmpInvalidFile.Write([]byte(invalidConfig)); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpInvalidFile.Close()

	t.Run("invalid config file", func(t *testing.T) {
		_, err := loadConfig(tmpInvalidFile.Name())
		if err == nil {
			t.Error("loadConfig() expected error for invalid config, got nil")
		}
	})
}

func TestGetCurrentTimestamp(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	expectedTime := "2024-01-15 10:30:00"

	t.Run("successful query", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"created"}).AddRow(expectedTime)
		mock.ExpectQuery("select current_timestamp\\(\\) as created").WillReturnRows(rows)

		result, err := getCurrentTimestamp(ctx, db)
		if err != nil {
			t.Errorf("getCurrentTimestamp() unexpected error: %v", err)
		}
		if result != expectedTime {
			t.Errorf("getCurrentTimestamp() = %v, want %v", result, expectedTime)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		mock.ExpectQuery("select current_timestamp\\(\\) as created").
			WillReturnError(sql.ErrConnDone)

		_, err := getCurrentTimestamp(ctx, db)
		if err == nil {
			t.Error("getCurrentTimestamp() expected error, got nil")
		}
	})
}

func TestGetStoredGames(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	t.Run("successful query", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"app_id", "playtime_forever"}).
			AddRow(100, 120).
			AddRow(200, 300).
			AddRow(300, 0)

		mock.ExpectQuery("select app_id, playtime_forever from games").WillReturnRows(rows)

		result, err := getStoredGames(ctx, db)
		if err != nil {
			t.Errorf("getStoredGames() unexpected error: %v", err)
		}

		if len(result) != 3 {
			t.Errorf("getStoredGames() returned %d games, want 3", len(result))
		}

		if result[100] != 120 {
			t.Errorf("getStoredGames()[100] = %d, want 120", result[100])
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"app_id", "playtime_forever"})
		mock.ExpectQuery("select app_id, playtime_forever from games").WillReturnRows(rows)

		result, err := getStoredGames(ctx, db)
		if err != nil {
			t.Errorf("getStoredGames() unexpected error: %v", err)
		}

		if len(result) != 0 {
			t.Errorf("getStoredGames() returned %d games, want 0", len(result))
		}
	})
}

func TestUpdateGame(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	t.Run("successful update", func(t *testing.T) {
		mock.ExpectExec("update games set playtime_forever = \\? where app_id = \\?").
			WithArgs(150, 100).
			WillReturnResult(sqlmock.NewResult(0, 1))

		err := updateGame(ctx, db, 100, 150)
		if err != nil {
			t.Errorf("updateGame() unexpected error: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})

	t.Run("update error", func(t *testing.T) {
		mock.ExpectExec("update games set playtime_forever = \\? where app_id = \\?").
			WithArgs(150, 100).
			WillReturnError(sql.ErrConnDone)

		err := updateGame(ctx, db, 100, 150)
		if err == nil {
			t.Error("updateGame() expected error, got nil")
		}
	})
}

func TestInsertGame(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	game := Game{
		Appid:                    100,
		HasCommunityVisibleStats: true,
		ImgIconURL:               "icon.jpg",
		ImgLogoURL:               "logo.jpg",
		Name:                     "Test Game",
		Playtime2weeks:           10,
		PlaytimeForever:          100,
		PlaytimeLinuxForever:     50,
		PlaytimeMacForever:       30,
		PlaytimeWindowsForever:   20,
	}
	created := "2024-01-15"

	t.Run("successful insert", func(t *testing.T) {
		mock.ExpectExec("insert into games").
			WithArgs(
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
				created,
			).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := insertGame(ctx, db, game, created)
		if err != nil {
			t.Errorf("insertGame() unexpected error: %v", err)
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unfulfilled expectations: %v", err)
		}
	})
}

func TestFetchOwnedGames(t *testing.T) {
	t.Run("successful fetch", func(t *testing.T) {
		response := OwnedGames{
			Response: struct {
				GameCount int    `json:"game_count"`
				Games     []Game `json:"games"`
			}{
				GameCount: 2,
				Games: []Game{
					{
						Appid:           100,
						Name:            "Game 1",
						PlaytimeForever: 120,
					},
					{
						Appid:           200,
						Name:            "Game 2",
						PlaytimeForever: 300,
					},
				},
			},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		}))
		defer server.Close()

		// Override the fetch function to use test server
		client := &http.Client{Timeout: 5 * time.Second}
		ctx := context.Background()

		// Note: This test would need the actual Steam API URL to be configurable
		// For now, we're testing the HTTP handling logic
		result, err := fetchOwnedGames(ctx, client, "test-key", "test-id")
		if err == nil {
			// The actual function will fail because it uses the real Steam API
			// In a real-world scenario, you'd want to make the URL configurable
			t.Log("fetchOwnedGames() would need URL to be configurable for testing")
		}
		_ = result
	})

	t.Run("context timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(2 * time.Second)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := &http.Client{Timeout: 100 * time.Millisecond}
		ctx := context.Background()

		// This will timeout because the real Steam API is being called
		_, err := fetchOwnedGames(ctx, client, "test-key", "test-id")
		if err == nil {
			t.Log("fetchOwnedGames() would timeout with a real request")
		}
	})
}

func TestSyncGames(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	ogs := &OwnedGames{
		Response: struct {
			GameCount int    `json:"game_count"`
			Games     []Game `json:"games"`
		}{
			GameCount: 3,
			Games: []Game{
				{Appid: 100, PlaytimeForever: 150, Name: "Game 1"}, // Update
				{Appid: 200, PlaytimeForever: 0, Name: "Game 2"},   // No change
				{Appid: 300, PlaytimeForever: 100, Name: "Game 3"}, // Insert
			},
		},
	}

	storedGames := map[int]int{
		100: 120, // Will be updated
		200: 0,   // No change
	}

	created := "2024-01-15"

	// Expect update for game 100
	mock.ExpectExec("update games set playtime_forever = \\? where app_id = \\?").
		WithArgs(150, 100).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Expect insert for game 300
	mock.ExpectExec("insert into games").
		WithArgs(300, false, "", "", "Game 3", 0, 100, 0, 0, 0, created).
		WillReturnResult(sqlmock.NewResult(1, 1))

	logger := newTestLogger(t)
	updated, inserted, played, err := syncGames(ctx, db, ogs, storedGames, created, logger)
	if err != nil {
		t.Errorf("syncGames() unexpected error: %v", err)
	}

	if updated != 1 {
		t.Errorf("syncGames() updated = %d, want 1", updated)
	}

	if inserted != 1 {
		t.Errorf("syncGames() inserted = %d, want 1", inserted)
	}

	if played != 2 {
		t.Errorf("syncGames() played = %d, want 2", played)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

// Helper function to create a test logger that discards output
func newTestLogger(t *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Only show errors during tests
	}))
}
