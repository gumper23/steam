package main

import (
	"fmt"
	"log"
	"time"

	"github.com/BurntSushi/toml"
)

type Steam struct {
	APIKey string `toml:"api_key"`
	ID     string `toml:"id"`
}

type Database struct {
	Hostname string `toml:"hostname"`
	Port     string `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
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

	log.Printf("%+v\n", config)
	log.Printf("[%s]\n", config.Database.Hostname)
	log.Printf("[%s]\n", config.Database.Username)
}
