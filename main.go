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
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
)

func main() {
	// 1. Load .env configuration
	LoadConfig()

	// 2. Set up SQLite session store with WAL mode for concurrent-write safety
	dbLog := waLog.Stdout("Database", "WARN", true)
	clientLog := waLog.Stdout("Client", "WARN", true)

	container, err := sqlstore.New(
		context.Background(),
		"sqlite",
		"file:whatsapp_session.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		dbLog,
	)
	if err != nil {
		panic(err)
	}

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		panic(err)
	}

	// 3. Create WhatsApp client and register event handler
	client := whatsmeow.NewClient(deviceStore, clientLog)
	setupEventHandler(client)

	// 4. Connect — show QR code on first run, reconnect silently on subsequent runs
	if client.Store.ID == nil {
		qrChan, _ := client.GetQRChannel(context.Background())
		err = client.Connect()
		if err != nil {
			panic(err)
		}
		for evt := range qrChan {
			if evt.Event == "code" {
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Println("👉 Scan the QR code using WhatsApp → Settings → Linked Devices → Link a Device")
			} else {
				fmt.Println("QR Event:", evt.Event)
			}
		}
	} else {
		err = client.Connect()
		if err != nil {
			panic(err)
		}
	}

	// 5. Block until Ctrl+C or SIGTERM
	fmt.Println("✅ Bot connected and listening. Press Ctrl+C to stop.")
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	fmt.Println("👋 Shutting down...")
	client.Disconnect()
}
