package main

import (
    "database/sql"
    "encoding/json"
    "fmt"
    _ "github.com/go-sql-driver/mysql"
    "io/ioutil"
    "net/http"
    "os"
    "sort"
)

type OwnedGamesWithNames struct {
    Response `json:"response"`
}

type Response struct {
    GameCount uint   `json:"game_count"`
    Games     Games `json:"games"`
}

type Game struct {
    Appid                    int    `json:"appid"`
    HasCommunityVisibleStats bool   `json:"has_community_visible_stats"`
    ImgIconURL               string `json:"img_icon_url"`
    ImgLogoURL               string `json:"img_logo_url"`
    Name                     string `json:"name"`
    PlaytimeForever          int    `json:"playtime_forever"`
}

type Games []Game

func (slice Games) Len() int {
    return len(slice)
}

func (slice Games) Less(i, j int) bool {
    return slice[i].PlaytimeForever < slice[j].PlaytimeForever
}

func (slice Games) Swap(i, j int) {
    slice[i], slice[j] = slice[j], slice[i]
}

func main() {
    steam_api_key := os.Getenv("STEAM_API_KEY")
    if steam_api_key == "" {
        panic("Environment variable STEAM_API_KEY not set")
    }

    steam_id := os.Getenv("STEAM_ID")
    if steam_id == "" {
        panic("Environment variable STEAM_ID not set")
    }

    url := "http://api.steampowered.com/IPlayerService/GetOwnedGames/v0001/?" +
           "key=" + steam_api_key +
           "&steamid=" + steam_id +
           "&include_appinfo=1" + 
           "&format=json"

    res, err := http.Get(url)
    if err != nil {
        panic(err)
    }
    defer res.Body.Close()

    body, err := ioutil.ReadAll(res.Body)
    if err != nil {
        panic(err)
    }
    // fmt.Printf("%s\n\n", body)

    var ogs OwnedGamesWithNames
    err = json.Unmarshal(body, &ogs)
    if err != nil {
        panic(err)
    }
    sort.Sort(ogs.Response.Games)

    mysql_username := os.Getenv("MYSQL_USERNAME")
    if mysql_username == "" {
        panic("Environment variable MYSQL_USERNAME not set")
    }

    mysql_password := os.Getenv("MYSQL_PASSWORD")
    if mysql_password == "" {
        panic("Environment variable MYSQL_PASSWORD not set")
    }

    db, err := sql.Open("mysql", 
                        mysql_username +
                        ":" +
                        mysql_password +
                        "@tcp(:3306)/steam")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // Get the current timestamp for this run.
    rows, err := db.Query("select current_timestamp() as created_at_ts")
    if err != nil {
        panic(err)
    }
    defer rows.Close()

    var created_at_ts string
    for rows.Next() {
        err = rows.Scan(&created_at_ts)
        if err != nil {
            panic(err)
        }
    }
    if created_at_ts == "" {
        panic("Unable to get a current timestamp?!")
    }

    sql := `
insert ignore into game
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

    for i, g := range ogs.Response.Games {
        fmt.Printf("Game = [%d], Playtime [%d], Name = [%s]\n", 
                   i + 1, 
                   g.PlaytimeForever, 
                   g.Name)
        _, err := db.Exec(sql, 
                          g.Appid, 
                          g.HasCommunityVisibleStats, 
                          g.ImgIconURL,
                          g.ImgLogoURL,
                          g.Name,
                          g.PlaytimeForever,
                          created_at_ts)
        if err != nil {
            panic(err)
       }
    }

    // fmt.Printf("%+v\n", ogs)
    // fmt.Printf("%v\n", ogs.Response.GameCount)
}

