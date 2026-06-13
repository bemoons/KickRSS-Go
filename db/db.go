package db

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"strings"

	"kickrss/config"
	_ "github.com/glebarez/go-sqlite"
)

var (
	DB *sql.DB
)

const SCHEMA = `
CREATE TABLE IF NOT EXISTS feeds (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    title           TEXT NOT NULL,
    url             TEXT NOT NULL UNIQUE,
    site_url        TEXT,
    etag            TEXT,
    last_modified   TEXT,
    last_fetched_at TEXT,
    seeded          INTEGER NOT NULL DEFAULT 0,
    enabled         INTEGER NOT NULL DEFAULT 1,
    need_classification INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS categories (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id     INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    is_default  INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT,
    UNIQUE(feed_id, name)
);

CREATE TABLE IF NOT EXISTS entries (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id       INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    category_id   INTEGER REFERENCES categories(id) ON DELETE SET NULL,
    guid          TEXT NOT NULL,
    title         TEXT NOT NULL,
    url           TEXT,
    author        TEXT,
    published_at  TEXT,
    fetched_at    TEXT,
    raw_content   TEXT,
    attention     TEXT,
    likely_no_text INTEGER DEFAULT 0,
    fulltext_ready INTEGER NOT NULL DEFAULT 0,
    is_read       INTEGER NOT NULL DEFAULT 0,
    read_at       TEXT,
    classified_at TEXT,
    is_starred    INTEGER NOT NULL DEFAULT 0,
    starred_at    TEXT,
    UNIQUE(feed_id, guid)
);

CREATE INDEX IF NOT EXISTS idx_entries_cat ON entries(category_id, is_read);
CREATE INDEX IF NOT EXISTS idx_entries_feed_unread ON entries(feed_id, is_read);
CREATE INDEX IF NOT EXISTS idx_entries_starred ON entries(is_starred);
CREATE INDEX IF NOT EXISTS idx_entries_fetched_at ON entries(fetched_at);

CREATE TABLE IF NOT EXISTS fulltext (
    entry_id   INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    content    TEXT,
    status     TEXT,
    fetched_at TEXT,
    fetcher    TEXT
);

CREATE TABLE IF NOT EXISTS summaries (
    entry_id   INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    content    TEXT NOT NULL,
    clickbait_note TEXT,
    model      TEXT,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    entry_id   INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_chat_messages_entry ON chat_messages(entry_id);

CREATE TABLE IF NOT EXISTS translations (
    entry_id   INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    content    TEXT NOT NULL,
    lang       TEXT NOT NULL,
    created_at TEXT
);

CREATE TABLE IF NOT EXISTS paragraph_translations (
    entry_id        INTEGER NOT NULL,
    para_index      INTEGER NOT NULL,
    lang            TEXT NOT NULL,
    original_text   TEXT NOT NULL,
    translated_text TEXT NOT NULL,
    created_at      TEXT,
    PRIMARY KEY (entry_id, para_index, lang),
    FOREIGN KEY (entry_id) REFERENCES entries (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS engagement (
    entry_id           INTEGER PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,
    opened             INTEGER NOT NULL DEFAULT 0,
    active_dwell_ms    INTEGER NOT NULL DEFAULT 0,
    scrolled_pct       REAL NOT NULL DEFAULT 0.0,
    scrolled_to_bottom INTEGER NOT NULL DEFAULT 0,
    opened_original    INTEGER NOT NULL DEFAULT 0,
    favorited          INTEGER NOT NULL DEFAULT 0,
    manual_bump        TEXT,
    recorded_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_interests (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    snapshot_date   TEXT NOT NULL UNIQUE,
    total_articles  INTEGER NOT NULL DEFAULT 0,
    high_engagement INTEGER NOT NULL DEFAULT 0,
    low_engagement  INTEGER NOT NULL DEFAULT 0,
    topics_json     TEXT NOT NULL,
    prompt_text     TEXT NOT NULL,
    generated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS token_usage (
    date              TEXT PRIMARY KEY,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0
);
`

func InitDB() error {
	dbPath := config.GetDBPath()
	log.Printf("Initializing database at %s", dbPath)

	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		db.Close()
		return err
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		db.Close()
		return err
	}

	if _, err := db.Exec(SCHEMA); err != nil {
		db.Close()
		return err
	}

	if err := migrateDatabase(db); err != nil {
		db.Close()
		return err
	}

	DB = db
	return nil
}

func migrateDatabase(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(entries);")
	if err != nil {
		return err
	}
	defer rows.Close()

	hasIsStarred := false
	hasStarredAt := false

	for rows.Next() {
		var cid int
		var name, dataType string
		var dfltVal sql.NullString
		var notnull, pk int
		if err := rows.Scan(&cid, &name, &dataType, &notnull, &dfltVal, &pk); err != nil {
			return err
		}
		if strings.ToLower(name) == "is_starred" {
			hasIsStarred = true
		}
		if strings.ToLower(name) == "starred_at" {
			hasStarredAt = true
		}
	}

	if !hasIsStarred {
		log.Println("Migrating database: adding is_starred column to entries table")
		if _, err := db.Exec("ALTER TABLE entries ADD COLUMN is_starred INTEGER NOT NULL DEFAULT 0;"); err != nil {
			return err
		}
	}
	if !hasStarredAt {
		log.Println("Migrating database: adding starred_at column to entries table")
		if _, err := db.Exec("ALTER TABLE entries ADD COLUMN starred_at TEXT;"); err != nil {
			return err
		}
	}

	rowsFeeds, err := db.Query("PRAGMA table_info(feeds);")
	if err != nil {
		return err
	}
	defer rowsFeeds.Close()

	hasNeedClassification := false
	for rowsFeeds.Next() {
		var cid int
		var name, dataType string
		var dfltVal sql.NullString
		var notnull, pk int
		if err := rowsFeeds.Scan(&cid, &name, &dataType, &notnull, &dfltVal, &pk); err != nil {
			return err
		}
		if strings.ToLower(name) == "need_classification" {
			hasNeedClassification = true
		}
	}

	if !hasNeedClassification {
		log.Println("Migrating database: adding need_classification column to feeds table")
		if _, err := db.Exec("ALTER TABLE feeds ADD COLUMN need_classification INTEGER NOT NULL DEFAULT 1;"); err != nil {
			return err
		}
	}

	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_entries_starred ON entries(is_starred);"); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_entries_fetched_at ON entries(fetched_at);"); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_chat_messages_entry ON chat_messages(entry_id);"); err != nil {
		return err
	}

	return nil
}

func CloseDB() {
	if DB != nil {
		DB.Close()
	}
}
