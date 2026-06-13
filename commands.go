package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
)

// CommandHandler is a function that handles a bot command and returns a reply string.
type CommandHandler func(sender, args string) string

// commands maps command names (without the "!" prefix) to their handlers.
var commands = map[string]CommandHandler{
	"help":     cmdHelp,
	"info":     cmdInfo,
	"rules":    cmdRules,
	"away":     cmdAway,
	"weather":  cmdWeather,
	"remindme": cmdReminder,
	"meme":     cmdMeme,
	"hangman":  cmdHangman,
}

// dispatchCommand checks if text is a command and dispatches it.
// Returns the reply and true if it was a command, or ("", false) otherwise.
func dispatchCommand(text, sender string) (string, bool) {
	cleaned := strings.TrimSpace(text)
	hasPrefix := strings.HasPrefix(cleaned, "!")
	rawCmdText := cleaned
	if hasPrefix {
		rawCmdText = strings.TrimPrefix(cleaned, "!")
	}

	parts := strings.SplitN(rawCmdText, " ", 2)
	cmd := strings.ToLower(strings.TrimSpace(parts[0]))
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	// 1. Exact command registration check
	if handler, ok := commands[cmd]; ok {
		return handler(sender, args), true
	}

	// 2. Active Hangman single-letter guess check (without prefix)
	if !hasPrefix && len(cleaned) == 1 {
		hangmanMutex.Lock()
		_, hasActiveHangman := hangmanGames[sender]
		hangmanMutex.Unlock()
		if hasActiveHangman {
			return cmdHangman(sender, cleaned), true
		}
	}

	// If it started with "!", we return "unknown command"
	if hasPrefix {
		return "❓ Unknown command. Type *!help* to see the menu.", true
	}

	// If no prefix and not matching any command/game state, it's not a command
	return "", false
}

// --- Command implementations ---

// cmdHelp displays the main menu of the bot.
func cmdHelp(sender, args string) string {
	return menuMessage
}

func cmdInfo(sender, args string) string {
	return `ℹ️ *About This Bot*

Built with *💚 by william*.

Features:
• Answers your DMs with AI
• Welcomes new group members
• Filters prohibited words
• Handles commands with !prefix

Version: 2.0.0`
}

func cmdRules(sender, args string) string {
	return `📋 *Group Rules*

1️⃣ Be respectful to all members
2️⃣ No spam or unsolicited promotion
3️⃣ Stay on topic
4️⃣ No hate speech or offensive content
5️⃣ Repeated violations may result in removal

Thank you for keeping this community great! 🙏`
}

// cmdAway toggles the global away mode and confirms the new state.
func cmdAway(sender, args string) string {
	Config.AwayMode = !Config.AwayMode
	if Config.AwayMode {
		return "✅ Away mode *ON* — DMs will receive an away message."
	}
	return "✅ Away mode *OFF* — AI replies are active."
}

// cmdWeather fetches real-time weather information from wttr.in.
func cmdWeather(sender, args string) string {
	city := strings.TrimSpace(args)
	if city == "" {
		return "⚠️ Please specify a city. Example: `!weather Nairobi`"
	}
	url := fmt.Sprintf("https://wttr.in/%s?format=3", strings.ReplaceAll(city, " ", "+"))
	resp, err := http.Get(url)
	if err != nil {
		return "⚠️ Failed to fetch weather. Please try again later."
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		return "⚠️ Weather data unavailable for that location."
	}
	return "🌦 *Weather Report*:\n" + strings.TrimSpace(string(body))
}

// cmdReminder schedules a reminder and messages the user when it triggers.
func cmdReminder(sender, args string) string {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) < 2 {
		return "⚠️ Format error. Use: `!remindme <duration> <message>`.\nExample: `!remindme 5m Buy milk`"
	}
	durationStr := parts[0]
	msg := parts[1]

	duration, err := time.ParseDuration(durationStr)
	if err != nil {
		val, err2 := strconv.Atoi(durationStr)
		if err2 == nil {
			duration = time.Duration(val) * time.Minute
		} else {
			return "⚠️ Invalid time duration. Use numbers followed by `s` (seconds), `m` (minutes), or `h` (hours). Example: `5m` or `1h`."
		}
	}

	jid, err := types.ParseJID(sender)
	if err != nil {
		return "⚠️ Failed to resolve your chat session JID."
	}

	go func() {
		time.Sleep(duration)
		if WhatsAppClient != nil {
			text := fmt.Sprintf("*🔔 [Reminder]*\n\nHey! Here is your reminder:\n👉 *%s*", msg)
			_, _ = WhatsAppClient.SendMessage(context.Background(), jid, &waE2E.Message{
				Conversation: &text,
			})
		}
	}()

	return fmt.Sprintf("✅ Reminder set! I will message you in *%s*.", duration.String())
}

// cmdMeme generates a link to a custom meme using api.memegen.link.
func cmdMeme(sender, args string) string {
	text := strings.TrimSpace(args)
	if text == "" {
		return "⚠️ Please specify some text. Example: `!meme hello|world` or `!meme buzz hello|world`"
	}

	// Default template
	templateName := "drake"

	// Check if user specified a template
	spaceParts := strings.SplitN(text, " ", 2)
	possibleTemplate := strings.ToLower(strings.TrimSpace(spaceParts[0]))

	validTemplates := map[string]bool{
		"drake": true, "buzz": true, "doge": true, "rollsafe": true,
		"grumpycat": true, "fry": true, "willywonka": true, "success": true,
	}

	var memeText string
	if validTemplates[possibleTemplate] && len(spaceParts) > 1 {
		templateName = possibleTemplate
		memeText = spaceParts[1]
	} else {
		memeText = text
	}

	parts := strings.Split(memeText, "|")
	top := parts[0]
	bottom := ""
	if len(parts) > 1 {
		bottom = parts[1]
	}

	escapeMeme := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.ReplaceAll(s, " ", "_")
		s = strings.ReplaceAll(s, "?", "~q")
		s = strings.ReplaceAll(s, "%", "~p")
		s = strings.ReplaceAll(s, "#", "~h")
		s = strings.ReplaceAll(s, "/", "~s")
		if s == "" {
			return "_"
		}
		return s
	}

	url := fmt.Sprintf("https://api.memegen.link/images/%s/%s/%s.png",
		templateName, escapeMeme(top), escapeMeme(bottom))

	return fmt.Sprintf("🎨 *Here is your custom meme (%s)*:\n%s", templateName, url)
}

// HangmanGame holds the state of a hangman match.
type HangmanGame struct {
	Word      string
	Guessed   map[rune]bool
	Attempts  int
	MaxErrors int
}

var (
	hangmanGames = make(map[string]*HangmanGame)
	hangmanMutex sync.Mutex
	hangmanWords = []string{
		"whatsapp", "golang", "programming", "developer", "computer",
		"internet", "database", "security", "application", "network",
	}
)

// cmdHangman plays a game of Hangman per chat user.
func cmdHangman(sender, args string) string {
	hangmanMutex.Lock()
	defer hangmanMutex.Unlock()

	game, ok := hangmanGames[sender]
	guess := strings.ToLower(strings.TrimSpace(args))

	if !ok || guess == "start" || guess == "reset" || guess == "" {
		word := hangmanWords[rand.Intn(len(hangmanWords))]
		hangmanGames[sender] = &HangmanGame{
			Word:      word,
			Guessed:   make(map[rune]bool),
			Attempts:  0,
			MaxErrors: 6,
		}
		return "🎮 *Hangman Started!*\n\n" + getHangmanDisplay(hangmanGames[sender]) + "\n\nGuess a letter by typing: `!hangman <letter>`"
	}

	if len(guess) != 1 {
		return "⚠️ Please guess exactly one letter. Example: `!hangman a`"
	}

	letter := rune(guess[0])
	if game.Guessed[letter] {
		return "⚠️ You already guessed the letter '" + string(letter) + "'.\n\n" + getHangmanDisplay(game)
	}

	game.Guessed[letter] = true

	correct := false
	for _, char := range game.Word {
		if char == letter {
			correct = true
			break
		}
	}

	if !correct {
		game.Attempts++
	}

	display := getHangmanDisplay(game)

	won := true
	for _, char := range game.Word {
		if !game.Guessed[char] {
			won = false
			break
		}
	}

	if won {
		delete(hangmanGames, sender)
		return fmt.Sprintf("🎉 *Congratulations! You Won!*\n\nThe word was: *%s*", game.Word)
	}

	if game.Attempts >= game.MaxErrors {
		delete(hangmanGames, sender)
		return fmt.Sprintf("💀 *Game Over! You Lost.*\n\nThe word was: *%s*\n\nType `!hangman` to start a new game.", game.Word)
	}

	status := "❌ Incorrect guess!"
	if correct {
		status = "✅ Correct guess!"
	}

	return fmt.Sprintf("🎮 *Hangman*\n\n%s\n\n%s\n\nRemaining Errors Allowed: *%d*", status, display, game.MaxErrors-game.Attempts)
}

func getHangmanDisplay(game *HangmanGame) string {
	var sb strings.Builder
	for _, char := range game.Word {
		if game.Guessed[char] {
			sb.WriteRune(char)
			sb.WriteString(" ")
		} else {
			sb.WriteString("_ ")
		}
	}
	return "`" + strings.TrimSpace(sb.String()) + "`"
}
