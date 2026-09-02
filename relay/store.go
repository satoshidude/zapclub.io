package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// openSQLite opens (or creates) the SQLite file at path.
// It returns separate writer and reader handles: WAL mode allows concurrent readers
// alongside the single writer (conductor tick goroutine).
func openSQLite(path string) (writer, reader *sql.DB, err error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"

	writer, err = sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	writer.SetMaxOpenConns(1) // one writer at a time

	// Verify WAL is actually active (modernc.org/sqlite ignores the old ?_journal_mode= URL form).
	var mode string
	if err = writer.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		writer.Close()
		return nil, nil, fmt.Errorf("sqlite journal_mode check: %v", err)
	}
	log.Printf("sqlite: journal_mode=%s path=%s", mode, path)

	if err = sqlInitSchema(writer); err != nil {
		writer.Close()
		return nil, nil, err
	}

	// Backfill played_at=0 rows (pre-migration) to the current time so the timestamp-based
	// matrix() logic treats them as "played before any queue version was written" — keeping
	// the offline-DJ replay guard intact after an upgrade from the boolean played-set.
	now := time.Now().UnixMilli()
	if _, err = writer.Exec(`UPDATE played SET played_at=? WHERE played_at=0`, now); err != nil {
		log.Printf("sqlite: played_at backfill: %v", err)
	}

	reader, err = sql.Open("sqlite", dsn+"&_pragma=query_only(true)")
	if err != nil {
		writer.Close()
		return nil, nil, err
	}
	reader.SetMaxOpenConns(4) // WAL allows concurrent readers

	log.Printf("sqlite: opened %s", path)
	return writer, reader, nil
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
			club      TEXT NOT NULL,
			dj        TEXT NOT NULL,
			video_id  TEXT NOT NULL,
			played_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (club, dj, video_id)
		);

		CREATE TABLE IF NOT EXISTS club_owners (
			club  TEXT PRIMARY KEY,
			owner TEXT NOT NULL
		);

	`)
	if err != nil {
		return err
	}

	// Guarded migration: add played_at column to existing DBs that predate this column.
	rows, err := db.Query(`PRAGMA table_info(played)`)
	if err != nil {
		return err
	}
	hasPlayedAt := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, dfltVal, pk interface{}
		if scanErr := rows.Scan(&cid, &name, &colType, &notNull, &dfltVal, &pk); scanErr == nil && name == "played_at" {
			hasPlayedAt = true
		}
	}
	rows.Close()
	if !hasPlayedAt {
		if _, err := db.Exec(`ALTER TABLE played ADD COLUMN played_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		log.Printf("sqlite: migrated played table: added played_at column")
	}
	return nil
}
