package main

import (
	"database/sql"
	"fmt"
	"log"

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
	db, err := sql.Open("sqlite", "./gallery.db")
	if err != nil {
		log.Fatal(err)
	}

	if err != nil {
		log.Fatal(err)
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS requests (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            url TEXT UNIQUE,
            created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
			aliases TEXT DEFAULT '[]' -- JSON array
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
