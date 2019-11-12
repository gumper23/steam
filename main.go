package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/BurntSushi/toml"
	_ "github.com/go-sql-driver/mysql"
)

type Game struct {
	Appid                    int64  `json:"appid"`
	HasCommunityVisibleStats bool   `json:"has_community_visible_stats"`
	ImgIconURL               string `json:"img_icon_url"`
	ImgLogoURL               string `json:"img_logo_url"`
	Name                     string `json:"name"`
	Playtime2weeks           int64  `json:"playtime_2weeks"`
	PlaytimeForever          int64  `json:"playtime_forever"`
	PlaytimeLinuxForever     int64  `json:"playtime_linux_forever"`
	PlaytimeMacForever       int64  `json:"playtime_mac_forever"`
	PlaytimeWindowsForever   int64  `json:"playtime_windows_forever"`
}

type OwnedGames struct {
	Response struct {
		GameCount int64  `json:"game_count"`
		Games     []Game `json:"games"`
	} `json:"response"`
}

type Steam struct {
	APIKey string `toml:"api_key"`
	ID     string `toml:"id"`
}

type Database struct {
	HostName   string `toml:"hostname"`
	Port       string `toml:"port"`
	UserName   string `toml:"username"`
	Password   string `toml:"password"`
	SchemaName string `toml:"schema_name"`
}

type Config struct {
	Database Database `toml:"database"`
	Steam    Steam    `toml:"steam"`
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
		log.Fatal(err)
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
		log.Fatal(err)
	}
	defer db.Close()

	if err = db.Ping(); err != nil {
		log.Fatal(err)
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

	var total, played int
	for _, game := range ogs.Response.Games {
		total++
		log.Printf("%+v\n", game)
		if game.PlaytimeForever != 0 {
			played++
		}
	}
	log.Printf("Total Games = %d, Played Games = %d, Played %% = %0.2f\n", total, played, float64(played)/float64(total)*100.0)

}
