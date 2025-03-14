package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

var (
	downloadDir = "./downloads"
	clients     = make(map[*websocket.Conn]bool)
	clientsMu   sync.Mutex
	photoChan   = make(chan string, 100) // Channel for photo paths to process
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func main() {
	if err := os.MkdirAll(downloadDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create download directory: %v", err)
	}

	db = initDB()
	if err := checkAndRedownloadMissingFiles(); err != nil {
		log.Printf("Error checking/redownloading files: %v", err)
	}

	// Start background tagging service
	go taggingService()

	r := gin.Default()
	r.Use(corsMiddleware())
	r.Static("/images", "./downloads")
	r.POST("/download", queueDownloads)
	r.GET("/photos", listPhotos)
	r.GET("/ws", handleWebSocket)

	log.Fatal(r.Run(":8080"))
}

// Background service to tag photos
func taggingService() {
	for {
		// Fetch untagged photos
		rows, err := db.Query(`
			SELECT p.file_path, r.url 
			FROM photos p 
			JOIN requests r ON p.request_id = r.id 
			WHERE p.file_path NOT IN (SELECT photo_path FROM photo_tags) 
			LIMIT 10`)
		if err != nil {
			log.Printf("Error querying untagged photos: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		// Queue all fetched photos
		photoCount := 0
		for rows.Next() {
			var filePath, galleryURL string
			if err := rows.Scan(&filePath, &galleryURL); err != nil {
				log.Printf("Error scanning photo: %v", err)
				continue
			}
			photoChan <- filePath
			photoCount++
		}
		rows.Close()

		// Process all queued photos in this batch
		for i := 0; i < photoCount; i++ {
			select {
			case photoPath := <-photoChan:
				if err := processPhotoForTagging(photoPath); err != nil {
					log.Printf("Error processing photo %s: %v", photoPath, err)
				}
			default:
				// If channel is empty prematurely, break out
				log.Printf("Channel empty before processing all %d photos", photoCount)
				break
			}
		}

		// If no photos were queued, wait before checking again
		if photoCount == 0 {
			log.Printf("No untagged photos found, waiting...")
			time.Sleep(10 * time.Second)
		} else {
			time.Sleep(2 * time.Second)
		}
	}
}

// Placeholder for photo processing and tagging
func processPhotoForTagging(filePath string) error {
	var galleryURL string
	err := db.QueryRow(`
        SELECT r.url 
        FROM photos p 
        JOIN requests r ON p.request_id = r.id 
        WHERE p.file_path = ?`, filePath).Scan(&galleryURL)
	if err != nil {
		return fmt.Errorf("fetching gallery URL for %s: %v", filePath, err)
	}

	personName := extractPersonNameFromURL(galleryURL)
	if personName == "" {
		return nil
	}

	// Handle main name
	var personID int
	err = db.QueryRow("SELECT id FROM people WHERE name = ?", personName).Scan(&personID)
	if err == sql.ErrNoRows {
		res, err := db.Exec("INSERT INTO people (name) VALUES (?)", personName)
		if err != nil {
			return fmt.Errorf("inserting person %s: %v", personName, err)
		}
		id, _ := res.LastInsertId()
		personID = int(id)
	} else if err != nil {
		return fmt.Errorf("querying person %s: %v", personName, err)
	}

	_, err = db.Exec("INSERT OR IGNORE INTO photo_tags (photo_path, person_id) VALUES (?, ?)", filePath, personID)
	if err != nil {
		return fmt.Errorf("tagging photo %s with person %d: %v", filePath, personID, err)
	}

	log.Printf("Tagged %s with person %s (ID: %d)", filePath, personName, personID)

	// Handle alias if present (e.g., "Kalena-A")
	re := regexp.MustCompile(`(.+?)\s*\((.+?)\)`)
	matches := re.FindStringSubmatch(strings.Split(galleryURL, "/threads/")[1])
	if len(matches) >= 3 {
		alias := strings.ReplaceAll(matches[2], "-", " ")
		alias = strings.TrimSpace(alias)
		if alias != "" && alias != personName {
			var aliasID int
			err = db.QueryRow("SELECT id FROM people WHERE name = ?", alias).Scan(&aliasID)
			if err == sql.ErrNoRows {
				res, err := db.Exec("INSERT INTO people (name) VALUES (?)", alias)
				if err != nil {
					return fmt.Errorf("inserting alias %s: %v", alias, err)
				}
				id, _ := res.LastInsertId()
				aliasID = int(id)
			}
			_, err = db.Exec("INSERT OR IGNORE INTO photo_tags (photo_path, person_id) VALUES (?, ?)", filePath, aliasID)
			if err != nil {
				return fmt.Errorf("tagging photo %s with alias %d: %v", filePath, aliasID, err)
			}
			log.Printf("Tagged %s with alias %s (ID: %d)", filePath, alias, aliasID)
		}
	}

	return nil
}

func extractPersonNameFromURL(url string) string {
	// Extract the thread part after "threads/"
	parts := strings.Split(url, "/threads/")
	if len(parts) < 2 {
		return ""
	}
	thread := parts[1]

	// Split by "-" and skip the numeric thread ID
	segments := strings.Split(thread, "-")
	if len(segments) < 3 { // Need at least ID, prefix/site, and name
		return ""
	}

	// Skip known site prefixes
	knownPrefixes := map[string]bool{
		"MetArt":     true,
		"MetArt-com": true,
		// Add more prefixes as needed (e.g., "Studio", "Playboy")
	}

	// Start after the thread ID (segments[0] is the ID)
	nameStartIdx := 1
	if knownPrefixes[segments[1]] || strings.HasSuffix(segments[1], "com") {
		nameStartIdx = 2 // Skip prefix like "MetArt-com"
	}

	// Collect name segments until we hit gallery details (e.g., resolution, date, or "x[digits]")
	var nameParts []string
	for i := nameStartIdx; i < len(segments); i++ {
		segment := segments[i]
		// Stop at gallery details: resolution (e.g., "3148x4720"), count (e.g., "x130"), or date (e.g., "(Jan-27-2024)")
		if regexp.MustCompile(`^\d+x\d+$`).MatchString(segment) || // e.g., "3148x4720"
			regexp.MustCompile(`^x\d+$`).MatchString(segment) || // e.g., "x130"
			strings.Contains(segment, "(") { // e.g., "(Jan-27-2024)"
			break
		}
		nameParts = append(nameParts, segment)
	}

	if len(nameParts) == 0 {
		return ""
	}

	// Join name parts and clean up
	name := strings.Join(nameParts, " ")
	name = strings.ReplaceAll(name, "-", " ") // Handle "Azzurra-Moretti" -> "Azzurra Moretti"
	name = strings.TrimSpace(name)

	// Handle aliases in parentheses if they appear in the name segment
	re := regexp.MustCompile(`(.+?)\s*\((.+?)\)`)
	matches := re.FindStringSubmatch(name)
	if len(matches) >= 2 {
		name = matches[1] // Take primary name before parentheses
	}

	// Validate: ensure it looks like a name (letters and spaces)
	if !regexp.MustCompile(`^[a-zA-Z\s]+$`).MatchString(name) {
		return ""
	}

	return name
}

func queueDownloads(c *gin.Context) {
	var req struct {
		URLs []string `json:"urls"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	go func() {
		for _, url := range req.URLs {
			if err := processURL(url); err != nil {
				log.Printf("Error processing URL %s: %v", url, err)
			}
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Download queued"})
}

func listPhotos(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	perPageStr := c.DefaultQuery("per_page", "50")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(perPageStr)
	if perPage < 1 {
		perPage = 50
	}

	type PhotoWithTags struct {
		RequestID int      `json:"RequestId"`
		URL       string   `json:"URL"`
		Path      string   `json:"Path"`
		Thumbnail string   `json:"Thumbnail"`
		CreatedAt string   `json:"CreatedAt"`
		Tags      []string `json:"Tags"`
	}

	var photos []PhotoWithTags
	rows, err := db.Query(`
        SELECT p.request_id, p.url, p.file_path, p.thumbnail_path, p.created_at, GROUP_CONCAT(pe.name, ',') 
        FROM photos p 
        LEFT JOIN photo_tags pt ON p.file_path = pt.photo_path 
        LEFT JOIN people pe ON pt.person_id = pe.id 
        GROUP BY p.file_path 
        ORDER BY p.created_at DESC 
        LIMIT ? OFFSET ?`,
		perPage, (page-1)*perPage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p PhotoWithTags
		var tags sql.NullString
		if err := rows.Scan(&p.RequestID, &p.URL, &p.Path, &p.Thumbnail, &p.CreatedAt, &tags); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if tags.Valid {
			p.Tags = strings.Split(tags.String, ",")
		}
		photos = append(photos, p)
	}

	var total int
	db.QueryRow("SELECT COUNT(*) FROM photos").Scan(&total)

	c.JSON(http.StatusOK, gin.H{
		"photos":      photos,
		"total":       total,
		"page":        pageStr,
		"per_page":    perPageStr,
		"total_pages": (total + perPage - 1) / perPage,
	})
}

func handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			clientsMu.Lock()
			delete(clients, conn)
			clientsMu.Unlock()
			break
		}
	}
}

func broadcastNewPhoto() {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	message := []byte(`{"event": "new_photo", "message": "New photo added"}`)
	for conn := range clients {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			log.Printf("WebSocket write error: %v", err)
			conn.Close()
			delete(clients, conn)
		}
	}
}

func checkAndRedownloadMissingFiles() error {
	rows, err := db.Query("SELECT url, file_path FROM photos")
	if err != nil {
		return fmt.Errorf("querying photos: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var url, filePath string
		if err := rows.Scan(&url, &filePath); err != nil {
			return fmt.Errorf("scanning photo: %v", err)
		}
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			log.Printf("File missing: %s, redownloading from %s", filePath, url)
			if err := DownloadFile(url, filePath); err != nil {
				log.Printf("Failed to redownload %s: %v", url, err)
			} else {
				log.Printf("Redownloaded %s to %s", url, filePath)
			}
		}
	}
	return nil
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func processURL(url string) error {
	fmt.Printf("Processing URL: %s\n", url)
	if err := DownloadGallery(url, url, ""); err != nil {
		return fmt.Errorf("error downloading gallery %s: %v", url, err)
	}
	return nil
}
