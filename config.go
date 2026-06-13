package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

// AppConfig holds all runtime configuration loaded from the .env file.
type AppConfig struct {
	GroqKey      string
	GroqModel    string
	AwayMode     bool
	AwayMessage  string
	BlockedWords []string
	AIAssist     bool
}

// ConfigMutex protects access to the global Config object.
var ConfigMutex sync.RWMutex
var Config AppConfig

// LoadConfig reads the .env file and populates the global Config.
func LoadConfig() {
	ConfigMutex.Lock()
	defer ConfigMutex.Unlock()

	if err := godotenv.Load(); err != nil {
		log.Println("ℹ️  No .env file found — using system environment variables")
	}

	Config.GroqKey = os.Getenv("GROQ_API_KEY")
	Config.GroqModel = os.Getenv("GROQ_MODEL")
	if Config.GroqModel == "" {
		Config.GroqModel = "llama-3.3-70b-versatile"
	}
	Config.AwayMode = strings.ToLower(os.Getenv("AWAY_MODE")) == "true"

	Config.AwayMessage = os.Getenv("AWAY_MESSAGE")
	if Config.AwayMessage == "" {
		Config.AwayMessage = "I'm away right now, I'll get back to you soon! 🕐"
	}

	Config.BlockedWords = nil
	blocked := os.Getenv("BLOCKED_WORDS")
	if blocked != "" {
		for _, w := range strings.Split(blocked, ",") {
			w = strings.TrimSpace(strings.ToLower(w))
			if w != "" {
				Config.BlockedWords = append(Config.BlockedWords, w)
			}
		}
	}

	Config.AIAssist = strings.ToLower(os.Getenv("AI_ASSIST")) != "false"

	log.Printf("✅ Config loaded | Away: %v | Blocked words: %d | Groq: %v | Model: %s | AI Assist: %v",
		Config.AwayMode, len(Config.BlockedWords), Config.GroqKey != "", Config.GroqModel, Config.AIAssist)
}

// GetConfig returns a safe copy of the current configuration.
func GetConfig() AppConfig {
	ConfigMutex.RLock()
	defer ConfigMutex.RUnlock()
	return Config
}

// UpdateConfig updates the configuration in memory and writes it to the .env file.
func UpdateConfig(awayMode bool, awayMessage string, blockedWords []string, groqModel string, aiAssist bool) error {
	ConfigMutex.Lock()
	Config.AwayMode = awayMode
	Config.AwayMessage = awayMessage
	Config.BlockedWords = blockedWords
	Config.AIAssist = aiAssist
	if groqModel != "" {
		Config.GroqModel = groqModel
	}
	key := Config.GroqKey
	model := Config.GroqModel
	ConfigMutex.Unlock()

	// Build the new .env content
	blockedStr := strings.Join(blockedWords, ",")
	content := fmt.Sprintf("GROQ_API_KEY=%s\nGROQ_MODEL=%s\nAWAY_MODE=%t\nAWAY_MESSAGE=%s\nBLOCKED_WORDS=%s\nAI_ASSIST=%t\n",
		key, model, awayMode, awayMessage, blockedStr, aiAssist)

	// Write back to .env to persist across restarts
	err := os.WriteFile(".env", []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("failed to save config to .env: %w", err)
	}
	return nil
}
