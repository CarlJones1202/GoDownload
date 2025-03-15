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

	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/clusters"
	"github.com/muesli/kmeans"
	_ "modernc.org/sqlite"
)

var (
	downloadDir = "./downloads"
	clients     = make(map[*websocket.Conn]bool)
	clientsMu   sync.Mutex
	photoChan   = make(chan string, 100) // For tagging
	colorChan   = make(chan string, 100) // For color extraction
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

	go taggingService()
	go colorExtractionService()

	r := gin.Default()
	r.Use(corsMiddleware())
	r.Static("/images", "./downloads")
	r.POST("/download", queueDownloads)
	r.GET("/photos", listPhotos)
	r.GET("/people", listPeople)
	r.POST("/people/combine", combinePeople)
	r.GET("/people/:id/photos", listPersonPhotos)
	r.POST("/people/:id/alias", addAlias)
	r.POST("/people/:id/profile-photo", setProfilePhoto)
	r.GET("/galleries", listGalleries)
	r.GET("/ws", handleWebSocket)

	log.Fatal(r.Run(":8080"))
}

// New color extraction service
func colorExtractionService() {
	for {
		rows, err := db.Query(`
            SELECT file_path 
            FROM photos 
            WHERE file_path NOT IN (SELECT photo_path FROM photo_colors) 
            LIMIT 10`)
		if err != nil {
			log.Printf("Error querying photos for color extraction: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		photoCount := 0
		for rows.Next() {
			var filePath string
			if err := rows.Scan(&filePath); err != nil {
				log.Printf("Error scanning photo for color: %v", err)
				continue
			}
			colorChan <- filePath
			photoCount++
		}
		rows.Close()

		for i := 0; i < photoCount; i++ {
			select {
			case photoPath := <-colorChan:
				if err := extractAndStoreColors(photoPath); err != nil {
					log.Printf("Error extracting colors for %s: %v", photoPath, err)
				}
			default:
				log.Printf("Color channel empty before processing all %d photos", photoCount)
				break
			}
		}

		if photoCount == 0 {
			log.Printf("No photos needing color extraction, waiting...")
			time.Sleep(10 * time.Second)
		}
	}
}

// Extract dominant colors from an image and store them
func extractAndStoreColors(filePath string) error {
	// Load image
	img, err := imaging.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening image %s: %v", filePath, err)
	}

	// Resize for faster processing
	img = imaging.Resize(img, 100, 0, imaging.Lanczos)

	// Convert pixels to colorful.Color
	bounds := img.Bounds()
	var observations clusters.Observations
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			c := colorful.Color{R: float64(r) / 65535.0, G: float64(g) / 65535.0, B: float64(b) / 65535.0}
			observations = append(observations, clusters.Coordinates{c.R, c.G, c.B})
		}
	}

	// Cluster colors using k-means (top 3 colors)
	km := kmeans.New()
	clusters, err := km.Partition(observations, 3)
	if err != nil {
		return fmt.Errorf("clustering colors: %v", err)
	}

	// Store the dominant colors
	for _, cluster := range clusters {
		center := cluster.Center
		hex := colorful.Color{R: center[0], G: center[1], B: center[2]}.Hex()
		_, err = db.Exec("INSERT OR IGNORE INTO photo_colors (photo_path, color_hex) VALUES (?, ?)", filePath, hex)
		if err != nil {
			return fmt.Errorf("storing color %s for %s: %v", hex, filePath, err)
		}
		log.Printf("Stored color %s for %s", hex, filePath)
	}

	return nil
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
func processPhotoForTagging(photoPath string) error {
	var galleryURL string
	err := db.QueryRow(`
        SELECT r.url 
        FROM photos p 
        JOIN requests r ON p.request_id = r.id 
        WHERE p.file_path = ?`, photoPath).Scan(&galleryURL)
	if err != nil {
		return fmt.Errorf("fetching gallery URL for %s: %v", photoPath, err)
	}
	log.Printf("Processing photo %s with gallery URL: %s", photoPath, galleryURL)

	name := extractPersonNameFromURL(galleryURL)
	if name == "" {
		log.Printf("No valid name extracted from %s", galleryURL)
		return nil
	}
	log.Printf("Extracted name for tagging: %s", name)

	var personID int
	err = db.QueryRow("SELECT id FROM people WHERE name = ?", name).Scan(&personID)
	if err == sql.ErrNoRows {
		res, err := db.Exec("INSERT INTO people (name) VALUES (?)", name)
		if err != nil {
			return fmt.Errorf("inserting person %s: %v", name, err)
		}
		id, _ := res.LastInsertId()
		personID = int(id)
		log.Printf("Created new person %s with ID %d", name, personID)
	} else if err != nil {
		return fmt.Errorf("fetching person %s: %v", name, err)
	}

	_, err = db.Exec(`
        INSERT OR IGNORE INTO photo_tags (photo_path, person_id) 
        VALUES (?, ?)`, photoPath, personID)
	if err != nil {
		return fmt.Errorf("tagging photo %s with person %d: %v", photoPath, personID, err)
	}

	log.Printf("Tagged %s with person %s (ID: %d) from gallery %s", photoPath, name, personID, galleryURL)
	return nil
}

func extractPersonNameFromURL(url string) string {
	// Extract thread part
	parts := strings.Split(url, "/threads/")
	if len(parts) < 2 {
		log.Printf("URL %s has no thread part", url)
		return ""
	}
	thread := parts[1]
	if strings.Contains(thread, "?") {
		thread = strings.Split(thread, "?")[0]
	}
	log.Printf("Thread: %s", thread)

	// Split by "-"
	segments := strings.Split(thread, "-")
	if len(segments) < 3 {
		log.Printf("URL %s has too few segments: %v", url, segments)
		return ""
	}
	log.Printf("Segments: %v", segments)

	// Skip thread ID and prefixes, including date (YYYY-MM-DD)
	startIdx := 1
	knownPrefixes := map[string]bool{
		"metart":     true,
		"metart-com": true,
		"studio":     true,
	}
	// Check for date pattern: YYYY-MM-DD (e.g., "2016-01-03")
	if startIdx+2 < len(segments) &&
		regexp.MustCompile(`^\d{4}$`).MatchString(segments[startIdx]) &&
		regexp.MustCompile(`^\d{2}$`).MatchString(segments[startIdx+1]) &&
		regexp.MustCompile(`^\d{2}$`).MatchString(segments[startIdx+2]) {
		log.Printf("Detected date pattern at index %d: %s-%s-%s", startIdx, segments[startIdx], segments[startIdx+1], segments[startIdx+2])
		startIdx += 3 // Skip past the date
	}
	// Skip known prefixes
	for i := startIdx; i < len(segments); i++ {
		lowerSeg := strings.ToLower(segments[i])
		if knownPrefixes[lowerSeg] || strings.HasSuffix(lowerSeg, "com") {
			startIdx = i + 1
		} else {
			break
		}
	}
	log.Printf("Name search starts at index %d (%s)", startIdx, segments[startIdx-1])

	// Collect name parts (max 2 words)
	var nameParts []string
	for i := startIdx; i < len(segments) && len(nameParts) < 2; i++ {
		segment := segments[i]
		lowerSeg := strings.ToLower(segment)

		// Early rejection: any segment with numbers
		if regexp.MustCompile(`\d`).MatchString(segment) {
			log.Printf("Segment %s (index %d) rejected: contains numbers", segment, i)
			break
		}

		// Stop at gallery details, metadata, or date indicators
		if regexp.MustCompile(`^\d+x\d+$`).MatchString(segment) || // e.g., "3456x5184"
			regexp.MustCompile(`^x\d+$`).MatchString(segment) || // e.g., "x120"
			strings.Contains(segment, "(") || // e.g., "(x120)"
			lowerSeg == "pictures" || lowerSeg == "px" || // e.g., "pictures"
			(i > startIdx && regexp.MustCompile(`^[A-Z][a-z]+$`).MatchString(segment)) { // e.g., "Madera"
			log.Printf("Stopping at segment %s (index %d)", segment, i)
			break
		}
		nameParts = append(nameParts, segment)
	}

	if len(nameParts) == 0 {
		log.Printf("No name parts found in %s after index %d", url, startIdx)
		return ""
	}
	log.Printf("Name parts: %v", nameParts)

	// Join and clean name
	name := strings.Join(nameParts, " ")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.TrimSpace(name)
	log.Printf("Candidate name: %s", name)

	// Double-check: reject if it contains any numbers
	if regexp.MustCompile(`\d`).MatchString(name) {
		log.Printf("Candidate %s rejected: contains numbers", name)
		return ""
	}

	// Validate: letters only
	if !regexp.MustCompile(`^[a-zA-Z\s]+$`).MatchString(name) {
		log.Printf("Name %s rejected: contains invalid characters", name)
		return ""
	}

	log.Printf("Final name for %s: %s", url, name)
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

	type PhotoWithTagsAndColors struct {
		RequestID int      `json:"RequestId"`
		URL       string   `json:"URL"`
		Path      string   `json:"Path"`
		Thumbnail string   `json:"Thumbnail"`
		CreatedAt string   `json:"CreatedAt"`
		Tags      []string `json:"Tags"`
		Colors    []string `json:"Colors"`
	}

	var photos []PhotoWithTagsAndColors
	rows, err := db.Query(`
        SELECT p.request_id, p.url, p.file_path, p.thumbnail_path, p.created_at, 
               GROUP_CONCAT(pe.name, ','), GROUP_CONCAT(pc.color_hex, ',') 
        FROM photos p 
        LEFT JOIN photo_tags pt ON p.file_path = pt.photo_path 
        LEFT JOIN people pe ON pt.person_id = pe.id 
        LEFT JOIN photo_colors pc ON p.file_path = pc.photo_path 
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
		var p PhotoWithTagsAndColors
		var tags, colors sql.NullString
		if err := rows.Scan(&p.RequestID, &p.URL, &p.Path, &p.Thumbnail, &p.CreatedAt, &tags, &colors); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if tags.Valid {
			p.Tags = strings.Split(tags.String, ",")
		}
		if colors.Valid {
			p.Colors = strings.Split(colors.String, ",")
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

func listPeople(c *gin.Context) {
	var people []struct {
		ID               int      `json:"id"`
		Name             string   `json:"name"`
		ProfilePhotoPath string   `json:"profilePhotoPath"`
		PhotoCount       int      `json:"photoCount"`
		Aliases          []string `json:"aliases"`
	}
	rows, err := db.Query(`
        SELECT p.id, p.name, 
               COALESCE(p.profile_photo_path, '') as profile_photo_path, 
               COUNT(pt.photo_path) as photo_count, 
               (SELECT GROUP_CONCAT(alias, ',') 
                FROM (SELECT DISTINCT a.alias 
                      FROM aliases a 
                      WHERE a.person_id = p.id)) as aliases
        FROM people p
        LEFT JOIN photo_tags pt ON p.id = pt.person_id
        GROUP BY p.id, p.name, COALESCE(p.profile_photo_path, '')
        ORDER BY p.name`)
	if err != nil {
		log.Printf("Query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p struct {
			ID               int      `json:"id"`
			Name             string   `json:"name"`
			ProfilePhotoPath string   `json:"profilePhotoPath"`
			PhotoCount       int      `json:"photoCount"`
			Aliases          []string `json:"aliases"`
		}
		var aliases sql.NullString
		err := rows.Scan(&p.ID, &p.Name, &p.ProfilePhotoPath, &p.PhotoCount, &aliases)
		if err != nil {
			log.Printf("Scan failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if aliases.Valid {
			p.Aliases = strings.Split(aliases.String, ",")
		} else {
			p.Aliases = []string{}
		}
		people = append(people, p)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Rows iteration failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, people)
}

func listPersonPhotos(c *gin.Context) {
	personID, _ := strconv.Atoi(c.Param("id"))
	var photos []Photo
	rows, err := db.Query(`
        SELECT p.request_id, p.url, p.file_path, p.thumbnail_path, p.created_at
        FROM photos p
        JOIN photo_tags pt ON p.file_path = pt.photo_path
        WHERE pt.person_id = ?
        ORDER BY p.created_at DESC`, personID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p Photo
		if err := rows.Scan(&p.RequestID, &p.URL, &p.Path, &p.Thumbnail, &p.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		photos = append(photos, p)
	}
	c.JSON(http.StatusOK, photos)
}

func combinePeople(c *gin.Context) {
	type CombineRequest struct {
		KeepID   int `json:"keepId"`
		DeleteID int `json:"deleteId"`
	}
	var req CombineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	// Reassign tags
	_, err = tx.Exec(`
        UPDATE photo_tags 
        SET person_id = ? 
        WHERE person_id = ?`, req.KeepID, req.DeleteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Reassign aliases
	_, err = tx.Exec(`
        INSERT OR IGNORE INTO aliases (person_id, alias)
        SELECT ?, alias FROM aliases WHERE person_id = ?`, req.KeepID, req.DeleteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	_, err = tx.Exec("DELETE FROM aliases WHERE person_id = ?", req.DeleteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Delete old person
	_, err = tx.Exec("DELETE FROM people WHERE id = ?", req.DeleteID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "People combined successfully"})
}

func addAlias(c *gin.Context) {
	personID, _ := strconv.Atoi(c.Param("id"))
	type AliasRequest struct {
		Alias string `json:"alias"`
	}
	var req AliasRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := db.Exec("INSERT OR IGNORE INTO aliases (person_id, alias) VALUES (?, ?)", personID, req.Alias)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Alias added successfully"})
}

func setProfilePhoto(c *gin.Context) {
	personID, _ := strconv.Atoi(c.Param("id"))
	type PhotoRequest struct {
		PhotoPath string `json:"photoPath"`
	}
	var req PhotoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify photo exists and is tagged with this person
	var count int
	err := db.QueryRow(`
        SELECT COUNT(*) 
        FROM photo_tags 
        WHERE photo_path = ? AND person_id = ?`, req.PhotoPath, personID).Scan(&count)
	if err != nil || count == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Photo not found or not tagged with this person"})
		return
	}

	_, err = db.Exec("UPDATE people SET profile_photo_path = ? WHERE id = ?", req.PhotoPath, personID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Profile photo updated"})
}

func listGalleries(c *gin.Context) {
	var galleries []struct {
		ID        int    `json:"id"`
		URL       string `json:"url"`
		CreatedAt string `json:"createdAt"`
	}
	rows, err := db.Query("SELECT id, url, created_at FROM requests ORDER BY created_at DESC")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var g struct {
			ID        int    `json:"id"`
			URL       string `json:"url"`
			CreatedAt string `json:"createdAt"`
		}
		if err := rows.Scan(&g.ID, &g.URL, &g.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		galleries = append(galleries, g)
	}
	c.JSON(http.StatusOK, galleries)
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
