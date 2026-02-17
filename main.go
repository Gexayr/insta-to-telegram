package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/robfig/cron/v3"
)

type ReelsResponse struct {
	Result struct {
		Edges []struct {
			Node struct {
				Media struct {
					Code      string `json:"code"`
					MediaType int    `json:"media_type"` // 2 = video/reel
				} `json:"media"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"result"`
}

var (
	bot *tgbotapi.BotAPI
	db  *sql.DB
)

func main() {
	var err error

	bot, err = tgbotapi.NewBotAPI(os.Getenv("BOT_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	db, err = sql.Open("sqlite3", "./data/data.db")
	if err != nil {
		log.Fatal(err)
	}

	initDB()

	c := cron.New()
	c.AddFunc(os.Getenv("CRON_SCHEDULE"), sendRandomLink)
	c.Start()

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			if update.Message.Text == "/send" {
				sendRandomLink()
			}
		}
	}

	select {}
}

func initDB() {
	query := `
	CREATE TABLE IF NOT EXISTS sent_links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		shortcode TEXT UNIQUE
	);
	`
	_, err := db.Exec(query)
	if err != nil {
		log.Fatal(err)
	}
}

func sendRandomLink() {
	profile := os.Getenv("INSTAGRAM_PROFILE")
	rapidAPIKey := os.Getenv("RAPIDAPI_KEY")

	body, _ := json.Marshal(map[string]string{
		"username": profile,
		"maxId":    "",
	})

	req, err := http.NewRequest("POST", "https://instagram120.p.rapidapi.com/api/instagram/reels", bytes.NewBuffer(body))
	if err != nil {
		log.Println("Failed to create request:", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-rapidapi-host", "instagram120.p.rapidapi.com")
	req.Header.Set("x-rapidapi-key", rapidAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Println("Request failed:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Unexpected status code: %d", resp.StatusCode)
		return
	}

	var data ReelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Println("Failed to decode response:", err)
		return
	}

	var candidates []string
	for _, edge := range data.Result.Edges {
		code := edge.Node.Media.Code
		mediaType := edge.Node.Media.MediaType
		if mediaType == 2 && !isSent(code) {
			candidates = append(candidates, code)
		}
	}

	if len(candidates) == 0 {
		log.Println("No new reels available")
		return
	}

	randomCode := candidates[rand.Intn(len(candidates))]
	saveSent(randomCode)

	link := fmt.Sprintf("https://www.instagram.com/reel/%s/", randomCode)
	log.Println("Sending link:", link)

	msg := tgbotapi.NewMessage(
		toInt64(os.Getenv("CHAT_ID")),
		link,
	)
	bot.Send(msg)
}

func isSent(shortcode string) bool {
	var exists string
	err := db.QueryRow("SELECT shortcode FROM sent_links WHERE shortcode = ?", shortcode).Scan(&exists)
	return err == nil
}

func saveSent(shortcode string) {
	_, err := db.Exec("INSERT INTO sent_links(shortcode) VALUES(?)", shortcode)
	if err != nil {
		log.Println(err)
	}
}

func toInt64(s string) int64 {
	var i int64
	fmt.Sscan(s, &i)
	return i
}
