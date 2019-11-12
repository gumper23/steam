package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/go-sql-driver/mysql"
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
	HostName   string `toml:"hostname"`
	Port       string `toml:"port"`
	UserName   string `toml:"username"`
	Password   string `toml:"password"`
	SchemaName string `toml:"schema_name"`
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

type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().UTC().Format("2006-01-02 15:04:05") + " " + string(bytes))
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	var config Config
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalln(err)
	}

	db, err := sql.Open("mysql",
		config.Database.UserName+
			":"+
			config.Database.Password+
			"@tcp("+
			config.Database.HostName+
			":"+
			config.Database.Port+
			")/"+
			config.Database.SchemaName)
	if err != nil {
		log.Fatalln(err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatalln(err)
	}

	rows, err := db.Query("select current_timestamp() as created")
	if err != nil {
		log.Fatalln(err)
	}
	defer rows.Close()

	var created string
	for rows.Next() {
		err = rows.Scan(&created)
		if err != nil {
			log.Fatalln(err)
		}
	}
	if err != nil {
		log.Fatalln(err)
	}
	rows.Close()

	rows, err = db.Query("select app_id, playtime_forever from games")
	if err != nil {
		log.Fatalln(err)
	}
	defer rows.Close()

	sgs := make(map[int]int)
	for rows.Next() {
		var sg StoredGame
		err = rows.Scan(&sg.Appid, &sg.PlaytimeForever)
		if err != nil {
			log.Fatalln(err)
		}
		sgs[sg.Appid] = sg.PlaytimeForever
	}

	url := "https://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/?key=" +
		config.Steam.APIKey +
		"&steamid=" +
		config.Steam.ID +
		"&include_appinfo=1" +
		"&format=json"
	log.Printf("\"%s\"\n", url)

	res, err := http.Get(url)
	if err != nil {
		log.Fatalln(err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log.Fatalln(err)
	}

	var ogs OwnedGames
	err = json.Unmarshal(body, &ogs)
	if err != nil {
		log.Fatalln(err)
	}

	var total, played, updated, inserted int
	for _, game := range ogs.Response.Games {
		total++
		if game.PlaytimeForever != 0 {
			played++
		}

		if playtime, ok := sgs[game.Appid]; ok {
			if playtime != game.PlaytimeForever {
				updated++
				_, err = db.Exec("update game set playtime_forever = " +
					strconv.Itoa(game.PlaytimeForever) +
					" where app_id = " +
					strconv.Itoa(game.Appid))
				if err != nil {
					log.Fatalln(err)
				}
			}
		} else {
			ins := `
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
			_, err = db.Exec(ins,
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
				log.Fatalln(err)
			}
			inserted++
		}
	}
	log.Printf("Total Games = %d, Played Games = %d, New Games = %d, Updated Playtime Games = %d, Played %% = %0.2f\n", total, played, inserted, updated, float64(played)/float64(total)*100.0)
}
