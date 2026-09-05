package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenSQLiteMigratesAutoPlaybackState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conductor.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE conductor_state (
			club TEXT PRIMARY KEY, pos INTEGER NOT NULL DEFAULT 0,
			video_id TEXT NOT NULL DEFAULT '', dj TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '', duration INTEGER NOT NULL DEFAULT 0,
			started_at INTEGER NOT NULL DEFAULT 0, playing INTEGER NOT NULL DEFAULT 0
		)`); err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	writer, reader, err := openSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	defer reader.Close()
	if _, err := writer.Exec(`
		INSERT INTO conductor_state(club, playing, auto) VALUES('club', 1, 1)
	`); err != nil {
		t.Fatalf("auto column was not migrated: %v", err)
	}
	var auto int
	if err := reader.QueryRow(`SELECT auto FROM conductor_state WHERE club='club'`).Scan(&auto); err != nil {
		t.Fatal(err)
	}
	if auto != 1 {
		t.Fatalf("stored auto state = %d, want 1", auto)
	}
}
