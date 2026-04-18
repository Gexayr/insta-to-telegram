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
	"time"

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
					MediaType int    `json:"media_type"`
				} `json:"media"`
			} `json:"node"`
		} `json:"edges"`
		MoreAvailable bool `json:"has_next_page"`
		PageInfo      struct {
			EndCursor string `json:"end_cursor"`
		} `json:"page_info"`
		NextMaxID string `json:"end_cursor"` // keep this for compatibility if it's also there
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
		if update.Message != nil && update.Message.Text == "/send" {
			sendRandomLink()
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
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT
	);`
	_, err := db.Exec(query)
	if err != nil {
		log.Fatal(err)
	}
}

func getConfig(key string) string {
	var value string
	err := db.QueryRow("SELECT value FROM config WHERE key = ?", key).Scan(&value)
	if err != nil {
		return ""
	}
	return value
}

func setConfig(key, value string) {
	_, err := db.Exec("INSERT INTO config(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, value)
	if err != nil {
		log.Println("Failed to save config:", err)
	}
}

func sendRandomLink() {
	profile := os.Getenv("INSTAGRAM_PROFILE")
	rapidAPIKey := os.Getenv("RAPIDAPI_KEY")

	maxID := getConfig("MAX_ID")
	if maxID == "" {
		maxID = os.Getenv("MAX_ID")
	}

	var candidates []string
	var lastMaxID string

	for range 15 { // limit pages to avoid abuse
		body, _ := json.Marshal(map[string]string{
			"username": profile,
			"maxId":    maxID,
		})

		req, err := http.NewRequest("POST",
			"https://instagram120.p.rapidapi.com/api/instagram/reels",
			bytes.NewBuffer(body),
		)
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

		if resp.StatusCode != http.StatusOK {
			log.Printf("Unexpected status code: %d", resp.StatusCode)
			resp.Body.Close()
			return
		}

		var data ReelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			log.Println("Failed to decode response:", err)
			resp.Body.Close()
			return
		}
		resp.Body.Close()

		for _, edge := range data.Result.Edges {
			code := edge.Node.Media.Code
			if edge.Node.Media.MediaType == 2 && !isSent(code) {
				candidates = append(candidates, code)
			}
		}

		lastMaxID = data.Result.PageInfo.EndCursor
		if lastMaxID == "" {
			lastMaxID = data.Result.NextMaxID
		}

		if !data.Result.MoreAvailable || lastMaxID == "" {
			break
		}

		maxID = lastMaxID
		time.Sleep(2 * time.Second) // avoid rate limit
	}

	if len(candidates) == 0 {
		log.Println("No new reels available")
		if lastMaxID != "" {
			log.Println("Updating MAX_ID to:", lastMaxID)
			setConfig("MAX_ID", lastMaxID)
		}
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
