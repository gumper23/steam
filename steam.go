package main

import (
    "database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
    "strconv"

	_ "github.com/go-sql-driver/mysql"
)

type OwnedGames struct {
	Response struct {
		GameCount int `json:"game_count"`
		Games     []struct {
			Appid                    int    `json:"appid"`
			HasCommunityVisibleStats bool   `json:"has_community_visible_stats"`
			ImgIconURL               string `json:"img_icon_url"`
			ImgLogoURL               string `json:"img_logo_url"`
			Name                     string `json:"name"`
			PlaytimeForever          int    `json:"playtime_forever"`
		} `json:"games"`
	} `json:"response"`
}

type StoredGame struct {
	Appid           int
	PlaytimeForever int
}

func main() {
    mysqlUsername := os.Getenv("MYSQL_USERNAME")
    if mysqlUsername == "" {
        panic("Environment variable MYSQL_USERNAME not set")
    }

    mysqlPassword := os.Getenv("MYSQL_PASSWORD")
    if mysqlPassword == "" {
        panic("Environment variable MYSQL_PASSWORD not set")
    }

    db, err := sql.Open("mysql",
                        mysqlUsername +
                        ":" +
                        mysqlPassword +
                        "@tcp(:3306)/steam")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    rows, err := db.Query("select current_timestamp() as created")
    if err != nil {
        panic(err)
    }
    defer rows.Close()

    var created string
    for rows.Next() {
        err = rows.Scan(&created)
        if err != nil {
            panic(err)
        }
    }
    if created == "" {
        panic("Invalid timestamp?!")
    }

    rows, err = db.Query("select app_id, playtime_forever from game")
    if err != nil {
        panic(err)
    }
    defer rows.Close()

    sgs := make(map[int]int)
    for rows.Next() {
        var sg StoredGame
        err = rows.Scan(&sg.Appid, &sg.PlaytimeForever)
        if err != nil {
            panic(err)
        }
        sgs[sg.Appid] = sg.PlaytimeForever
    }
    fmt.Println(sgs)

	steamAPIKey := os.Getenv("STEAM_API_KEY")
	if steamAPIKey == "" {
		panic("Environment variable STEAM_API_KEY not set")
	}

	steamID := os.Getenv("STEAM_ID")
	if steamID == "" {
		panic("Environment variable STEAM_ID not set")
	}

	url := "http://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/?key=" +
		steamAPIKey +
		"&steamid=" +
		steamID +
		"&include_appinfo=1" +
		"&format=json"
	// fmt.Println(url)

	res, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	// fmt.Println(body)

	var ogs OwnedGames
	err = json.Unmarshal(body, &ogs)
	if err != nil {
		panic(err)
	}

	var total, played, inserted, updated int
	for i, g := range ogs.Response.Games {
		total++
		if g.PlaytimeForever > 0 {
			played++
		}
		fmt.Printf("Game = [%d], Playtime = [%d], Name = [%s]\n",
			i+1,
			g.PlaytimeForever,
			g.Name)

        if playtime, ok := sgs[g.Appid]; ok {
            if playtime != g.PlaytimeForever {
                updated++
                fmt.Println("Updating " + g.Name)
                _, err = db.Exec("update game set playtime_forever = " +
                                 strconv.Itoa(g.PlaytimeForever) +
                                 " where app_id = " +
                                 strconv.Itoa(g.Appid))
                if err != nil {
                    panic(err)
                }
            }
        } else {
            fmt.Println("Inserting " + g.Name)
            inserted++
            ins := `
insert into game
(
    app_id
    , has_community_visible_stats
    , img_icon_url
    , img_logo_url
    , name
    , playtime_forever
    , created_at_ts
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
)`
            _, err = db.Exec(ins,
                             g.Appid,
                             g.HasCommunityVisibleStats,
                             g.ImgIconURL,
                             g.ImgLogoURL,
                             g.Name,
                             g.PlaytimeForever,
                             created)
            if err != nil {
                panic(err)
            }
        }
	}

	fmt.Printf("Total games = [%d], Played = [%d], Played %% = [%.2g]\n",
               total,
		       played,
		       float64(played)/float64(total)*100)
    fmt.Printf("Inserted = [%d], Updated = [%d]\n", inserted, updated)

	fmt.Println("Done!\n")
}
