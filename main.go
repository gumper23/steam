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
	return fmt.Print(time.Now().Format("2006-01-02 15:04:05") + " " + string(bytes))
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(logWriter))
	log.Println("Beginning execution")

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
	rows.Close()

	url := "https://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/?key=" +
		config.Steam.APIKey +
		"&steamid=" +
		config.Steam.ID +
		"&include_appinfo=1" +
		"&format=json"
	// log.Printf("\"%s\"\n", url)

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
				_, err = db.Exec("update games set playtime_forever = " +
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
	log.Println("Execution complete")
}
