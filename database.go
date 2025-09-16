package main

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Photo struct {
	RequestID int
	URL       string
	Path      string
	Thumbnail string
	CreatedAt string
}

var db *sql.DB

func initDB() *sql.DB {
	db, err := sql.Open("sqlite", "./gallery.db?_busy_timeout=5000")
	if err != nil {
		log.Fatal(err)
	}

	db.Exec("PRAGMA journal_mode=WAL;")
	// Recommend synchronous normal for better performance with WAL
	db.Exec("PRAGMA synchronous=NORMAL;")

	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS requests (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            url TEXT UNIQUE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
        );
		CREATE TABLE IF NOT EXISTS studios (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE
		);
		CREATE TABLE IF NOT EXISTS galleries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id INTEGER,
            studio_id INTEGER,
			name TEXT,
            FOREIGN KEY (request_id) REFERENCES requests(id),
            FOREIGN KEY (studio_id) REFERENCES studios(id)
		);
        CREATE TABLE IF NOT EXISTS photos (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            request_id INTEGER,
            url TEXT,
            file_path TEXT,
            thumbnail_path TEXT,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            FOREIGN KEY (request_id) REFERENCES requests(id)
        );
		CREATE TABLE IF NOT EXISTS favorites (
			photo_id INTEGER PRIMARY KEY,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (photo_id) REFERENCES photos(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS similarity_feedback (
			source_photo_id INTEGER,
			target_photo_id INTEGER,
			is_similar BOOLEAN NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (source_photo_id, target_photo_id),
			FOREIGN KEY (source_photo_id) REFERENCES photos(id) ON DELETE CASCADE,
			FOREIGN KEY (target_photo_id) REFERENCES photos(id) ON DELETE CASCADE
		);
        CREATE TABLE IF NOT EXISTS photo_colors (
            photo_path TEXT,
            color_hex TEXT,
            FOREIGN KEY (photo_path) REFERENCES photos(file_path),
            PRIMARY KEY (photo_path, color_hex)
        );
        CREATE TABLE IF NOT EXISTS aliases (
            person_id INTEGER,
            alias TEXT,
            FOREIGN KEY (person_id) REFERENCES people(id),
            PRIMARY KEY (person_id, alias)
        );
		CREATE TABLE IF NOT EXISTS people (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			aliases TEXT DEFAULT '[]', -- JSON array
			profile_photo_id INTEGER
		);
		CREATE TABLE IF NOT EXISTS photo_tags (
			photo_path TEXT NOT NULL,
			person_id INTEGER NOT NULL,
			FOREIGN KEY (person_id) REFERENCES people(id)
		);`)
	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec("UPDATE requests SET status = 'pending' WHERE status = 'processing'")
	if err != nil {
		log.Fatal(err)
	}
	return db
}

func storePhoto(requestURL, photoURL, filePath, thumbnailPath string) error {
	var requestID int
	err := db.QueryRow("SELECT id FROM requests WHERE url = ?", requestURL).Scan(&requestID)
	if err == sql.ErrNoRows {
		result, err := db.Exec("INSERT INTO requests (url) VALUES (?)", requestURL)
		if err != nil {
			return fmt.Errorf("inserting request %s: %v", requestURL, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("getting request ID: %v", err)
		}
		requestID = int(id)
	} else if err != nil {
		return fmt.Errorf("querying request %s: %v", requestURL, err)
	}

	_, err = db.Exec(`
		INSERT INTO photos (request_id, url, file_path, thumbnail_path) 
		VALUES (?, ?, ?, ?)`,
		requestID, photoURL, filePath, thumbnailPath)
	if err != nil {
		return fmt.Errorf("inserting photo %s: %v", photoURL, err)
	}

	broadcastNewPhoto()
	return nil
}

// execWithRetry executes a query with retries on SQLITE_BUSY
func execWithRetry(query string, args ...interface{}) (sql.Result, error) {
	var (
		res sql.Result
		err error
	)
	for i := 0; i < 5; i++ {
		res, err = db.Exec(query, args...)
		if err == nil {
			return res, nil
		}
		if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "SQLITE_BUSY") {
			time.Sleep(time.Duration(100*(i+1)) * time.Millisecond)
			continue
		}
		return nil, err
	}
	return res, err
}
