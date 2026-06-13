package main

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// AppConfig holds all runtime configuration loaded from the .env file.
type AppConfig struct {
	GeminiKey    string
	AwayMode     bool
	AwayMessage  string
	BlockedWords []string
}

// Config is the global configuration instance.
var Config AppConfig

// LoadConfig reads the .env file and populates the global Config.
func LoadConfig() {
	if err := godotenv.Load(); err != nil {
		log.Println("ℹ️  No .env file found — using system environment variables")
	}

	Config.GeminiKey = os.Getenv("GEMINI_API_KEY")
	Config.AwayMode = strings.ToLower(os.Getenv("AWAY_MODE")) == "true"

	Config.AwayMessage = os.Getenv("AWAY_MESSAGE")
	if Config.AwayMessage == "" {
		Config.AwayMessage = "I'm away right now, I'll get back to you soon! 🕐"
	}

	blocked := os.Getenv("BLOCKED_WORDS")
	if blocked != "" {
		for _, w := range strings.Split(blocked, ",") {
			w = strings.TrimSpace(strings.ToLower(w))
			if w != "" {
				Config.BlockedWords = append(Config.BlockedWords, w)
			}
		}
	}

	log.Printf("✅ Config loaded | Away: %v | Blocked words: %d | Gemini: %v",
		Config.AwayMode, len(Config.BlockedWords), Config.GeminiKey != "")
}
