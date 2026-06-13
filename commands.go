package main

import "strings"

// CommandHandler is a function that handles a bot command and returns a reply string.
type CommandHandler func(sender, args string) string

// commands maps command names (without the "!" prefix) to their handlers.
var commands = map[string]CommandHandler{
	"help":  cmdHelp,
	"info":  cmdInfo,
	"rules": cmdRules,
	"away":  cmdAway,
}

// dispatchCommand checks if text is a "!" command and dispatches it.
// Returns the reply and true if it was a command, or ("", false) otherwise.
func dispatchCommand(text, sender string) (string, bool) {
	if !strings.HasPrefix(text, "!") {
		return "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(text, "!"), " ", 2)
	cmd := strings.ToLower(strings.TrimSpace(parts[0]))
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	if handler, ok := commands[cmd]; ok {
		return handler(sender, args), true
	}
	return "❓ Unknown command. Type *!help* to see available commands.", true
}

// --- Command implementations ---

func cmdHelp(sender, args string) string {
	return `🤖 *Bot Commands*

!help  — Show this help message
!info  — About this bot
!rules — View group rules
!away  — Toggle away mode on/off`
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
