package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

var userStatesDB *sql.DB

// InitUserStatesDB opens user_states.db and ensures the table exists.
func InitUserStatesDB() {
	var err error
	userStatesDB, err = sql.Open("sqlite", "file:user_states.db?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatalf("❌ Failed to open user states database: %v", err)
	}

	query := `
	CREATE TABLE IF NOT EXISTS user_states (
		jid TEXT PRIMARY KEY,
		state TEXT
	);`
	_, err = userStatesDB.Exec(query)
	if err != nil {
		log.Fatalf("❌ Failed to create user_states table: %v", err)
	}
	log.Println("✅ User states database initialized successfully")
}

// GetUserState returns the current state for a JID, or "new" if it doesn't exist.
func GetUserState(jid string) string {
	if userStatesDB == nil {
		return "new"
	}
	var state string
	query := "SELECT state FROM user_states WHERE jid = ?"
	err := userStatesDB.QueryRow(query, jid).Scan(&state)
	if err != nil {
		if err == sql.ErrNoRows {
			return "new"
		}
		log.Printf("⚠️ Error querying user state for %s: %v", jid, err)
		return "new"
	}
	return state
}

// SetUserState updates or inserts a user's state.
func SetUserState(jid, state string) {
	if userStatesDB == nil {
		return
	}
	query := `
	INSERT INTO user_states (jid, state) VALUES (?, ?)
	ON CONFLICT(jid) DO UPDATE SET state = excluded.state;`
	_, err := userStatesDB.Exec(query, jid, state)
	if err != nil {
		log.Printf("❌ Failed to update user state for %s: %v", jid, err)
	}
}
