package main

import (
	"context"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// setupEventHandler registers the central event router on the WhatsApp client.
func setupEventHandler(c *whatsmeow.Client) {
	c.AddEventHandler(func(evt interface{}) {
		handleEvent(c, evt)
	})
}

// handleEvent routes raw WhatsApp events to the appropriate handler.
func handleEvent(c *whatsmeow.Client, evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		handleMessage(c, v)
	case *events.GroupInfo:
		handleGroupInfo(c, v)
	}
}

// handleMessage processes every incoming message with a decision chain:
//
//	own message → drop
//	newsletter  → log only
//	blocked word → warn (group) or ignore (DM)
//	!command    → dispatch to commands.go
//	group msg   → commands only, no AI
//	DM + away   → send away message
//	DM          → Gemini AI reply
func handleMessage(c *whatsmeow.Client, v *events.Message) {
	// 1. Drop own messages to prevent echo loops
	if v.Info.IsFromMe {
		return
	}

	// 2. Read-only WhatsApp Channels — log only, no reply possible
	if v.Info.Chat.Server == types.NewsletterServer {
		fmt.Printf("[Channel] %s: %s\n", v.Info.Chat.String(), extractText(v))
		return
	}

	text := extractText(v)
	if text == "" {
		return // Non-text message (image, sticker, etc.) — ignore
	}

	sender := v.Info.Sender.String()
	isGroup := v.Info.IsGroup

	fmt.Printf("[%s] %s: %s\n", chatLabel(isGroup), sender, text)

	// 3. Blocked word filter
	if blockedWord := findBlockedWord(text); blockedWord != "" {
		fmt.Printf("🚫 Blocked word '%s' from %s\n", blockedWord, sender)
		if isGroup {
			sendText(c, v.Info.Chat, "⚠️ Message blocked: contains prohibited content.")
		}
		return
	}

	// 4. Command dispatch (!help, !info, !rules, !away)
	if reply, ok := dispatchCommand(text, sender); ok {
		sendText(c, v.Info.Chat, reply)
		return
	}

	// 5. Group messages only get command support — no AI in groups
	if isGroup {
		return
	}

	// 6. Personal DM — away mode takes priority
	if Config.AwayMode {
		sendText(c, v.Info.Chat, Config.AwayMessage)
		return
	}

	// 7. Personal DM — ask Gemini AI
	reply, err := askGemini(text)
	if err != nil {
		fmt.Printf("❌ Gemini error: %v\n", err)
		sendText(c, v.Info.Chat, "🤖 *AI assistant is temporarily unavailable.* Please try again later.")
		return
	}
	sendText(c, v.Info.Chat, reply)
}

// handleGroupInfo fires when group metadata changes, including member joins.
func handleGroupInfo(c *whatsmeow.Client, v *events.GroupInfo) {
	for _, joined := range v.Join {
		// v.Join is []types.JID — use the phone number portion as the display name
		name := joined.ToNonAD().User
		welcome := fmt.Sprintf(
			"👋 Welcome to the group, *%s*! Great to have you here.\n\nType *!rules* to see the group rules or *!help* to see what I can do. 🎉",
			name,
		)
		sendText(c, v.JID, welcome)
	}
}

// --- Helpers ---

// extractText pulls plain text from a message, supporting both regular
// and extended (reply/link preview) text messages.
func extractText(v *events.Message) string {
	if v.Message.Conversation != nil {
		return strings.TrimSpace(*v.Message.Conversation)
	}
	if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.Text != nil {
		return strings.TrimSpace(*v.Message.ExtendedTextMessage.Text)
	}
	return ""
}

// findBlockedWord returns the first blocked word found in text, or "".
func findBlockedWord(text string) string {
	lower := strings.ToLower(text)
	for _, word := range Config.BlockedWords {
		if strings.Contains(lower, word) {
			return word
		}
	}
	return ""
}

// sendText sends a plain text WhatsApp message to a JID.
func sendText(c *whatsmeow.Client, to types.JID, text string) {
	_, err := c.SendMessage(context.Background(), to, &waE2E.Message{
		Conversation: &text,
	})
	if err != nil {
		fmt.Printf("❌ Failed to send message to %s: %v\n", to.String(), err)
	}
}

// chatLabel returns a human-readable label for log lines.
func chatLabel(isGroup bool) string {
	if isGroup {
		return "Group"
	}
	return "DM"
}
