package main

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

// openSQLite opens (or creates) the SQLite file at path and initialises the schema.
// WAL mode allows concurrent reads alongside the single writer (conductor tick goroutine).
func openSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: one writer at a time
	if err := sqlInitSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	log.Printf("sqlite: opened %s", path)
	return db, nil
}

func sqlInitSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS conductor_state (
			club       TEXT PRIMARY KEY,
			pos        INTEGER NOT NULL DEFAULT 0,
			video_id   TEXT NOT NULL DEFAULT '',
			dj         TEXT NOT NULL DEFAULT '',
			title      TEXT NOT NULL DEFAULT '',
			duration   INTEGER NOT NULL DEFAULT 0,
			started_at INTEGER NOT NULL DEFAULT 0,
			playing    INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS played (
			club     TEXT NOT NULL,
			dj       TEXT NOT NULL,
			video_id TEXT NOT NULL,
			PRIMARY KEY (club, dj, video_id)
		);

		CREATE TABLE IF NOT EXISTS club_owners (
			club  TEXT PRIMARY KEY,
			owner TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS premium_cache (
			pubkey  TEXT PRIMARY KEY,
			valid   INTEGER NOT NULL DEFAULT 0,
			expires INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS radio_enabled (
			club    TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0
		);
	`)
	return err
}
