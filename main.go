package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "modernc.org/sqlite"
	"github.com/mdp/qrterminal/v3"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
)

var client *whatsmeow.Client

// eventHandler processes real-time events pushed by WhatsApp's backend
func eventHandler(evt interface{}) {
	switch v := evt.(type) {
	case *events.Message:
		// Drop messages sent by the bot itself to prevent infinite loops
		if v.Info.IsFromMe {
			return
		}

		// WhatsApp Channels (newsletters) are read-only broadcast feeds.
		// Attempting to reply returns HTTP 401 — skip silently.
		if v.Info.Chat.Server == types.NewsletterServer {
			fmt.Printf("[Channel] %s: %s\n", v.Info.Chat.String(), func() string {
				if v.Message.Conversation != nil {
					return *v.Message.Conversation
				}
				return "(non-text)"
			}())
			return
		}

		// Extract text content from the incoming payload envelope
		var incomingText string
		if v.Message.Conversation != nil {
			incomingText = *v.Message.Conversation
		} else if v.Message.ExtendedTextMessage != nil && v.Message.ExtendedTextMessage.Text != nil {
			incomingText = *v.Message.ExtendedTextMessage.Text
		}

		if incomingText == "" {
			return
		}

		fmt.Printf("Received message from %s: %s\n", v.Info.Sender.String(), incomingText)

		// Construct a text message packet to reply back
		replyPayload := &waE2E.Message{
			Conversation: &[]string{"Echo: " + incomingText}[0],
		}

		// Route the message to the sender's target Chat JID (Jabber ID string format)
		_, err := client.SendMessage(context.Background(), v.Info.Chat, replyPayload)
		if err != nil {
			fmt.Printf("Failed to reply: %v\n", err)
		}
	}
}

func main() {
	// 1. Setup logs
	dbLog := waLog.Stdout("Database", "WARN", true)
	clientLog := waLog.Stdout("Client", "WARN", true)

	// 2. Initialize localized session store using standard SQLite
	container, err := sqlstore.New(context.Background(), "sqlite", "file:whatsapp_session.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbLog)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	// 3. Construct WhatsApp client configuration instances
	client = whatsmeow.NewClient(deviceStore, clientLog)
	client.AddEventHandler(eventHandler)

	// 4. Authenticate session state via QR code generation if unlinked
	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				// Render a visible, scannable terminal graphic layout
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("👉 Scan the QR code above using your WhatsApp application link menu!")
			} else {
				fmt.Println("Authentication Event Success Range:", evt.Event)
			}
		}
	} else {
		// Existing token is found in SQLite database container, reconnect immediately
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	// 5. Keep runtime processing background execution alive until interrupt signal
	fmt.Println("WhatsApp framework linked successfully. Listening for chats...")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	client.Disconnect()
}
