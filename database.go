package main

import (
	"database/sql"
	"fmt"
	"strconv"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDB() error {
	var err error
	db, err = sql.Open("sqlite", "./downloads.db")
	if err != nil {
		return err
	}

	// Create tables
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
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (request_id) REFERENCES requests(id)
		);
	`)
	return err
}

func storeRequest(url string) error {
	_, err := db.Exec("INSERT OR IGNORE INTO requests (url) VALUES (?)", url)
	return err
}

func storePhoto(requestURL, photoURL, filePath string) error {
	var requestID int
	err := db.QueryRow("SELECT id FROM requests WHERE url = ?", requestURL).Scan(&requestID)
	if err != nil {
		return fmt.Errorf("getting request ID for %s: %v", requestURL, err)
	}
	_, err = db.Exec("INSERT INTO photos (request_id, url, file_path) VALUES (?, ?, ?)", requestID, photoURL, filePath)
	return err
}

func getPhotos(page, perPage string) ([]map[string]string, int64, error) {
	// Convert page and perPage from string to int with defaults
	p, err := strconv.Atoi(page)
	if err != nil || p < 1 {
		p = 1
	}
	pp, err := strconv.Atoi(perPage)
	if err != nil || pp < 1 {
		pp = 10
	}

	offset := (p - 1) * pp

	// Get total count
	var total int64
	err = db.QueryRow("SELECT COUNT(*) FROM photos").Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Get paged photos
	rows, err := db.Query(`
		SELECT r.url AS request_url, p.url, p.file_path, p.created_at 
		FROM photos p 
		JOIN requests r ON p.request_id = r.id 
		ORDER BY p.created_at DESC 
		LIMIT ? OFFSET ?`, pp, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var photos []map[string]string
	for rows.Next() {
		var reqURL, url, filePath, createdAt string
		if err := rows.Scan(&reqURL, &url, &filePath, &createdAt); err != nil {
			return nil, 0, err
		}
		photos = append(photos, map[string]string{
			"request_url": reqURL,
			"url":         url,
			"file_path":   filePath,
			"created_at":  createdAt,
		})
	}
	return photos, total, nil
}
