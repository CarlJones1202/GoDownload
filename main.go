package main

import (
	"awesomeProject/similarity"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/disintegration/imaging"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/lucasb-eyer/go-colorful"
	"github.com/muesli/clusters"
	"github.com/muesli/kmeans"
	_ "modernc.org/sqlite"
)

var (
	downloadDir     = "./downloads"
	clients         = make(map[*websocket.Conn]bool)
	clientsMu       sync.Mutex
	photoChan       = make(chan string, 100) // For tagging
	colorChan       = make(chan string, 100) // For color extraction
	similarityModel *similarity.SimilarityModel
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type UpdatePersonRequest struct {
	Name    string   `json:"name" binding:"required"`
	Aliases []string `json:"aliases" binding:"required"` // Require aliases to ensure we always set them
	PhotoId *int     `json:"photoId,omitempty"`
}

func main() {
	if err := os.MkdirAll(downloadDir, os.ModePerm); err != nil {
		log.Fatalf("Failed to create download directory: %v", err)
	}

	db = initDB()

	go func() {
		for {
			if err := checkAndRedownloadMissingFiles(); err != nil {
				log.Printf("Redownload check failed: %v", err)
			}
		}
	}()
	go taggingService()
	// go colorExtractionService()
	go processPendingDownloads() // New background service

	r := gin.Default()
	r.Use(corsMiddleware())
	r.Static("/images", "./downloads")
	r.POST("/download", queueDownloads)
	r.GET("/photos", listPhotos)
	r.GET("/people", listPeople)
	r.PUT("/people/:id", updatePerson)
	r.POST("/people/combine", combinePeople)
	r.GET("/people/:id/photos", listPersonPhotos)
	r.POST("/people/:id/alias", addAlias)
	r.POST("/people/:id/profile-photo", setProfilePhoto)
	r.GET("/people/search", searchStashDBPeople)
	r.POST("/people", addPerson)
	r.GET("/galleries", listGalleries)
	r.GET("/ws", handleWebSocket)
	r.DELETE("/galleries/:id", deleteGallery)                     // Route for deleting galleries
	r.POST("/galleries/:id/assign-person", assignPersonToGallery) // Route for assigning person to gallery
	r.DELETE("/photos/:id", deletePhoto)

	r.POST("/photos/:id/favorite", favoritePhoto)
	r.DELETE("/photos/:id/favorite", unfavoritePhoto)
	r.POST("/photos/:id/similarity-feedback", provideSimilarityFeedback)
	r.GET("/photos/:id/similar", getSimilarPhotos)
	r.GET("/photos/:id/feedback-candidates", getFeedbackCandidates)

	log.Fatal(r.Run(":8081"))
}

func addPerson(c *gin.Context) {
	var req Person
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Default to empty slice if aliases is nil
	if req.Aliases == nil {
		req.Aliases = []string{}
	}

	aliasesJSON, err := json.Marshal(req.Aliases)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode aliases"})
		return
	}

	result, err := db.Exec("INSERT INTO people (name, aliases) VALUES (?, ?)", req.Name, aliasesJSON)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add person: " + err.Error()})
		return
	}
	id, err := result.LastInsertId()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get person ID"})
		return
	}

	person := Person{
		ID:      int(id),
		Name:    req.Name,
		Aliases: req.Aliases,
	}
	c.JSON(http.StatusOK, person)
}

func updatePerson(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person ID"})
		return
	}

	var req UpdatePersonRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	if req.Aliases == nil {
		req.Aliases = []string{}
	}

	aliasesJSON, err := json.Marshal(req.Aliases)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode aliases"})
		return
	}

	var profilePhotoID *int
	if req.PhotoId != nil {
		// Validate the photo belongs to this person
		var count int
		err := db.QueryRow(`
            SELECT COUNT(*) FROM photos p
            JOIN photo_tags pt ON p.file_path = pt.photo_path
            WHERE p.id = ? AND pt.person_id = ?`, *req.PhotoId, id).Scan(&count)
		if err != nil || count == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Photo not found or not tagged with this person"})
			return
		}
		profilePhotoID = req.PhotoId
	}

	// Build update query
	if profilePhotoID != nil {
		_, err = db.Exec("UPDATE people SET name = ?, aliases = ?, profile_photo_id = ? WHERE id = ?", req.Name, aliasesJSON, *profilePhotoID, id)
	} else {
		_, err = db.Exec("UPDATE people SET name = ?, aliases = ? WHERE id = ?", req.Name, aliasesJSON, id)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update person: " + err.Error()})
		return
	}

	// Materialize profilePhotoPath for response
	var profilePhotoPath string
	if profilePhotoID != nil {
		_ = db.QueryRow("SELECT file_path FROM photos WHERE request_id = ?", *profilePhotoID).Scan(&profilePhotoPath)
	}

	person := Person{
		ID:      id,
		Name:    req.Name,
		Aliases: req.Aliases,
	}

	c.JSON(http.StatusOK, person)
}

func processPendingDownloads() {
	for {
		rows, err := db.Query("SELECT id, url FROM requests WHERE status = 'pending' LIMIT 1")
		if err != nil {
			log.Printf("Error querying pending requests: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		var id int
		var url string
		hasNext := rows.Next()
		if !hasNext {
			rows.Close()
			time.Sleep(10 * time.Second)
			continue
		}

		err = rows.Scan(&id, &url)
		rows.Close()
		if err != nil {
			log.Printf("Error scanning pending request: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}

		// Mark as processing
		_, err = db.Exec("UPDATE requests SET status = 'processing' WHERE id = ?", id)
		if err != nil {
			log.Printf("Error marking request %d as processing: %v", id, err)
			time.Sleep(2 * time.Second)
			continue
		}

		// Process the URL
		log.Printf("Processing request %d: %s", id, url)
		err = processURL(url)
		if err != nil {
			log.Printf("Failed to process URL %s: %v", url, err)
			_, err = db.Exec("UPDATE requests SET status = 'failed' WHERE id = ?", id)
			if err != nil {
				log.Printf("Error marking request %d as failed: %v", id, err)
			}
		} else {
			_, err = db.Exec("UPDATE requests SET status = 'completed' WHERE id = ?", id)
			if err != nil {
				log.Printf("Error marking request %d as completed: %v", id, err)
			}
			log.Printf("Completed processing request %d: %s", id, url)
		}

		time.Sleep(2 * time.Second) // Small delay between processing
	}
}

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
				break
			}
		}

		if photoCount == 0 {
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
	clusters, err := km.Partition(observations, 6)
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

		for i := 0; i < photoCount; i++ {
			select {
			case photoPath := <-photoChan:
				err := processPhotoForTagging(photoPath)
				if err != nil && err != ErrNoPersonMatch {
					log.Printf("Error processing photo %s: %v", photoPath, err)
				} else if err == ErrNoPersonMatch {
					log.Printf("No matching person found for photo %s, will retry later", photoPath)
				}
				// If ErrNoPersonMatch, do nothing: it will be retried next time
			default:
				break
			}
		}

		if photoCount == 0 {
			time.Sleep(10 * time.Second)
		} else {
			time.Sleep(2 * time.Second)
		}
	}
}

func queueDownloads(c *gin.Context) {
	var req struct {
		URL string `json:"url"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if strings.HasSuffix(req.URL, "[range]") {
		baseUrl := strings.TrimSuffix(req.URL, "[range]")
		postUrls, err := enumerateAllPostUrls(baseUrl)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enumerate posts: " + err.Error()})
			return
		}
		count := 0
		for _, postUrl := range postUrls {
			_, err := db.Exec(
				"INSERT INTO requests (url, created_at, status) VALUES (?, ?, 'pending') ON CONFLICT(url) DO NOTHING",
				postUrl, time.Now().Format(time.RFC3339),
			)
			if err == nil {
				count++
			}
		}
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Queued %d posts for download", count)})
		return
	}

	// Single post as before
	_, err := db.Exec(
		"INSERT INTO requests (url, created_at, status) VALUES (?, ?, 'pending') ON CONFLICT(url) DO NOTHING",
		req.URL, time.Now().Format(time.RFC3339),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to queue download: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Download queued"})
}

// Helper: enumerate all post URLs in a thread (across all pages)
func enumerateAllPostUrls(baseUrl string) ([]string, error) {
	var postUrls []string
	seen := make(map[string]bool)
	page := 1
	for {
		pageUrl := baseUrl
		if page > 1 {
			if strings.Contains(baseUrl, "?") {
				pageUrl = fmt.Sprintf("%s&page=%d", baseUrl, page)
			} else {
				pageUrl = fmt.Sprintf("%s/page%d", strings.TrimRight(baseUrl, "/"), page)
			}
		}
		resp, err := http.Get(pageUrl)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %v", pageUrl, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s: %v", pageUrl, err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			return nil, fmt.Errorf("parsing HTML from %s: %v", pageUrl, err)
		}

		foundNew := false
		doc.Find("div[id^='post_message_']").Each(func(_ int, s *goquery.Selection) {
			id, exists := s.Attr("id")
			if !exists || seen[id] {
				return
			}
			seen[id] = true
			foundNew = true
			// Compose post URL using the actual pageUrl (not baseUrl)
			postUrl := pageUrl
			if strings.Contains(pageUrl, "#") {
				postUrl = strings.Split(pageUrl, "#")[0]
			}
			postUrl = fmt.Sprintf("%s#%s", postUrl, id)
			postUrls = append(postUrls, postUrl)
		})

		if !foundNew {
			break // No new posts found, stop
		}
		page++
	}
	return postUrls, nil
}

func listPhotos(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	perPageStr := c.DefaultQuery("per_page", "50")
	personIDStr := c.Query("person_id")
	galleryIDStr := c.Query("gallery_id")
	tag := c.Query("tag")
	color := c.Query("color")

	page, _ := strconv.Atoi(pageStr)
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(perPageStr)
	if perPage < 1 {
		perPage = 50
	}

	query := `
        SELECT p.id, p.request_id, p.url, p.file_path, p.thumbnail_path, p.created_at, 
               GROUP_CONCAT(pe.name, ','), GROUP_CONCAT(pc.color_hex, ',') 
        FROM photos p 
        LEFT JOIN photo_tags pt ON p.file_path = pt.photo_path 
        LEFT JOIN people pe ON pt.person_id = pe.id 
        LEFT JOIN photo_colors pc ON p.file_path = pc.photo_path
    `
	countQuery := "SELECT COUNT(DISTINCT p.file_path) FROM photos p"
	whereClauses := []string{}
	args := []interface{}{}

	// Filter by person_id
	if personIDStr != "" {
		personID, err := strconv.Atoi(personIDStr)
		if err == nil {
			query += " JOIN photo_tags pt_person ON p.file_path = pt_person.photo_path"
			countQuery += " JOIN photo_tags pt_person ON p.file_path = pt_person.photo_path"
			whereClauses = append(whereClauses, "pt_person.person_id = ?")
			args = append(args, personID)
		}
	}

	// Filter by gallery_id (request_id)
	if galleryIDStr != "" {
		galleryID, err := strconv.Atoi(galleryIDStr)
		if err == nil {
			whereClauses = append(whereClauses, "p.request_id = ?")
			args = append(args, galleryID)
		}
	}

	// Filter by tag (person name or alias)
	if tag != "" {
		query += " LEFT JOIN aliases a ON pe.id = a.person_id"
		countQuery += " LEFT JOIN photo_tags pt_tag ON p.file_path = pt_tag.photo_path"
		countQuery += " LEFT JOIN people pe_tag ON pt_tag.person_id = pe_tag.id"
		countQuery += " LEFT JOIN aliases a ON pe_tag.id = a.person_id"
		whereClauses = append(whereClauses, "(pe.name LIKE ? OR a.alias LIKE ?)")
		args = append(args, "%"+tag+"%", "%"+tag+"%")
	}

	// Filter by color (simple exact match for now, similarity below)
	if color != "" {
		query += " JOIN photo_colors pc_color ON p.file_path = pc_color.photo_path"
		countQuery += " JOIN photo_colors pc_color ON p.file_path = pc_color.photo_path"
		r, g, b, err := hexToRGB(color)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid color hex"})
			return
		}
		// Allow ±20 range for each RGB component
		whereClauses = append(whereClauses, `
			ABS(CAST('0x' || SUBSTR(pc_color.color_hex, 2, 2) AS INTEGER) - ?) <= 20 AND
			ABS(CAST('0x' || SUBSTR(pc_color.color_hex, 4, 2) AS INTEGER) - ?) <= 20 AND
			ABS(CAST('0x' || SUBSTR(pc_color.color_hex, 6, 2) AS INTEGER) - ?) <= 20`)
		args = append(args, r, g, b)
	}

	if len(whereClauses) > 0 {
		query += " WHERE " + strings.Join(whereClauses, " AND ")
		countQuery += " WHERE " + strings.Join(whereClauses, " AND ")
	}

	query += `
        GROUP BY p.file_path 
        ORDER BY p.request_id DESC, p.created_at DESC 
        LIMIT ? OFFSET ?`
	args = append(args, perPage, (page-1)*perPage)

	rows, err := db.Query(query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	var photos []PhotoWithTagsAndColors
	for rows.Next() {
		var p PhotoWithTagsAndColors
		var tags, colors sql.NullString
		if err := rows.Scan(&p.Id, &p.RequestID, &p.URL, &p.Path, &p.Thumbnail, &p.CreatedAt, &tags, &colors); err != nil {
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
	err = db.QueryRow(countQuery, args[:len(args)-2]...).Scan(&total) // Exclude LIMIT/OFFSET for count
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"photos":      photos,
		"total":       total,
		"page":        pageStr,
		"per_page":    perPageStr,
		"total_pages": (total + perPage - 1) / perPage,
	})
}

// hexToRGB converts a hex color to RGB values
func hexToRGB(hex string) (r, g, b int, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color: %s", hex)
	}
	rVal, err := strconv.ParseInt(hex[0:2], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	gVal, err := strconv.ParseInt(hex[2:4], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	bVal, err := strconv.ParseInt(hex[4:6], 16, 32)
	if err != nil {
		return 0, 0, 0, err
	}
	return int(rVal), int(gVal), int(bVal), nil
}

func listPeople(c *gin.Context) {
	var people []Person
	rows, err := db.Query(`
        SELECT p.id, p.name, 
               COUNT(pt.photo_path) as photo_count,
               COUNT(DISTINCT ph.request_id) as gallery_count,
               p.aliases,
               p.profile_photo_id
        FROM people p
        LEFT JOIN photo_tags pt ON p.id = pt.person_id
        LEFT JOIN photos ph ON pt.photo_path = ph.file_path
        GROUP BY p.id, p.name
        ORDER BY p.name`)
	if err != nil {
		log.Printf("Query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	for rows.Next() {
		var p Person
		var aliasesJSON sql.NullString
		var profilePhotoID sql.NullInt64
		err := rows.Scan(&p.ID, &p.Name, &p.PhotoCount, &p.GalleryCount, &aliasesJSON, &profilePhotoID)
		if err != nil {
			log.Printf("Scan failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if aliasesJSON.Valid && aliasesJSON.String != "" && aliasesJSON.String != `""` {
			if err := json.Unmarshal([]byte(aliasesJSON.String), &p.Aliases); err != nil {
				log.Printf("Failed to unmarshal aliases '%s' for %s (ID: %d): %v", aliasesJSON.String, p.Name, p.ID, err)
				p.Aliases = []string{}
			}
		} else {
			p.Aliases = []string{}
		}

		if profilePhotoID.Valid {
			var thumbPath string
			var id int
			err := db.QueryRow("SELECT id, thumbnail_path FROM photos WHERE id = ?", int(profilePhotoID.Int64)).Scan(&id, &thumbPath)
			if err == nil {
				p.ProfilePhoto = &PhotoSummary{
					ID:        id,
					Thumbnail: thumbPath,
				}
			}
		}
		people = append(people, p)
	}
	if err = rows.Err(); err != nil {
		log.Printf("Rows iteration failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Returning %d people", len(people))
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

type GalleryWithPeople struct {
	ID        int      `json:"id"`
	URL       string   `json:"url"`
	CreatedAt string   `json:"createdAt"`
	Thumbnail string   `json:"thumbnail,omitempty"`
	People    []Person `json:"people"`
}

func listGalleries(c *gin.Context) {
	personIDStr := c.Query("person_id")

	var galleries []GalleryWithPeople

	query := `
        SELECT DISTINCT r.id, r.url, r.created_at, MIN(p.thumbnail_path) as thumbnail
        FROM requests r
        JOIN photos p ON r.id = p.request_id
    `
	var args []interface{}

	if personIDStr != "" {
		personID, err := strconv.Atoi(personIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid person_id"})
			return
		}
		query += `
			JOIN photo_tags pt ON p.file_path = pt.photo_path
			WHERE pt.person_id = ?
		`
		args = append(args, personID)
	}

	query += `
		GROUP BY r.id, r.url, r.created_at
		ORDER BY r.created_at DESC
	`

	rows, err := db.Query(query, args...)
	if err != nil {
		log.Printf("Query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	galleryMap := make(map[int]*GalleryWithPeople)
	var galleryIDs []int

	for rows.Next() {
		var g GalleryWithPeople
		var thumbnail sql.NullString
		if err := rows.Scan(&g.ID, &g.URL, &g.CreatedAt, &thumbnail); err != nil {
			log.Printf("Scan failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if thumbnail.Valid {
			g.Thumbnail = thumbnail.String
		}
		g.People = []Person{}
		galleries = append(galleries, g)
		galleryMap[g.ID] = &galleries[len(galleries)-1]
		galleryIDs = append(galleryIDs, g.ID)
	}

	if len(galleryIDs) == 0 {
		c.JSON(http.StatusOK, galleries)
		return
	}

	// Fetch all people for all galleries in one query
	placeholders := make([]string, len(galleryIDs))
	args2 := make([]interface{}, len(galleryIDs))
	for i, id := range galleryIDs {
		placeholders[i] = "?"
		args2[i] = id
	}
	peopleQuery := `
        SELECT DISTINCT p.request_id, pe.id, pe.name, pe.aliases
        FROM photos p
        JOIN photo_tags pt ON p.file_path = pt.photo_path
        JOIN people pe ON pt.person_id = pe.id
        WHERE p.request_id IN (` + strings.Join(placeholders, ",") + `)
    `
	peopleRows, err := db.Query(peopleQuery, args2...)
	if err != nil {
		log.Printf("Error fetching people for galleries: %v", err)
		// Still return galleries with empty people arrays
		c.JSON(http.StatusOK, galleries)
		return
	}
	defer peopleRows.Close()

	for peopleRows.Next() {
		var requestID, personID int
		var name string
		var aliasesJSON sql.NullString
		if err := peopleRows.Scan(&requestID, &personID, &name, &aliasesJSON); err != nil {
			log.Printf("Error scanning person for gallery %d: %v", requestID, err)
			continue
		}
		var aliases []string
		if aliasesJSON.Valid && aliasesJSON.String != "" && aliasesJSON.String != `""` {
			if err := json.Unmarshal([]byte(aliasesJSON.String), &aliases); err != nil {
				aliases = []string{}
			}
		} else {
			aliases = []string{}
		}
		if g, ok := galleryMap[requestID]; ok {
			g.People = append(g.People, Person{
				ID:      personID,
				Name:    name,
				Aliases: aliases,
			})
		}
	}

	log.Printf("Returning %d galleries", len(galleries))
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

const maxConcurrentDownloads = 5

func checkAndRedownloadMissingFiles() error {
	// Fetch all rows into memory to release the database connection quickly
	type photo struct {
		url      string
		filePath string
	}
	var photos []photo

	rows, err := db.Query("SELECT url, file_path FROM photos ORDER BY created_at DESC")
	if err != nil {
		return fmt.Errorf("querying photos: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var url, filePath string
		if err := rows.Scan(&url, &filePath); err != nil {
			return fmt.Errorf("scanning photo: %v", err)
		}
		photos = append(photos, photo{url: url, filePath: filePath})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("reading rows: %v", err)
	}

	// Filter missing files
	var tasks []photo
	for _, p := range photos {
		if _, err := os.Stat(p.filePath); os.IsNotExist(err) {
			tasks = append(tasks, p)
		}
	}

	if len(tasks) == 0 {
		return nil
	}

	// Semaphore to limit concurrency
	sem := make(chan struct{}, maxConcurrentDownloads)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errors []error

	// Process tasks concurrently
	for _, task := range tasks {
		wg.Add(1)
		sem <- struct{}{} // Acquire semaphore slot
		go func(url, filePath string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore slot

			log.Printf("File missing: %s, redownloading from %s", filePath, url)
			if err := DownloadFile(url, filePath); err != nil {
				log.Printf("Failed to redownload %s: %v", url, err)
				mu.Lock()
				errors = append(errors, fmt.Errorf("redownload %s: %v", url, err))
				mu.Unlock()
				return
			}

			log.Printf("Redownloaded %s to %s", url, filePath)
			filename := path.Base(filePath)
			directory := filepath.Dir(filePath)
			thumbnailDir := directory + "/thumbnails"
			if err := os.MkdirAll(thumbnailDir, 0755); err != nil {
				log.Printf("Failed to create thumbnail dir %s: %v", thumbnailDir, err)
				mu.Lock()
				errors = append(errors, fmt.Errorf("create thumbnail dir %s: %v", thumbnailDir, err))
				mu.Unlock()
				return
			}

			thumbnailPath := fmt.Sprintf("%s/thumb_%s", thumbnailDir, filename)
			if err := generateThumbnail(filePath, thumbnailPath); err != nil {
				log.Printf("Error generating thumbnail for %s: %v", filePath, err)
				mu.Lock()
				errors = append(errors, fmt.Errorf("generate thumbnail %s: %v", filePath, err))
				mu.Unlock()
			} else {
				log.Printf("Generated thumbnail: %s", thumbnailPath)
			}
		}(task.url, task.filePath)
	}

	// Wait for all downloads to complete
	wg.Wait()

	// Report aggregated errors
	if len(errors) > 0 {
		errMsg := "Errors during redownload:\n"
		for _, err := range errors {
			errMsg += fmt.Sprintf("- %v\n", err)
		}
		return fmt.Errorf(errMsg)
	}

	log.Printf("Completed redownloading %d files", len(tasks))
	return nil
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

type Gallery struct {
	ID        int    `json:"id"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

func deleteGallery(c *gin.Context) {
	galleryIDStr := c.Param("id")
	galleryID, err := strconv.Atoi(galleryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gallery id"})
		return
	}

	// Get all photo file paths for this gallery
	rows, err := db.Query("SELECT file_path, thumbnail_path FROM photos WHERE request_id = ?", galleryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query photos: " + err.Error()})
		return
	}
	var filePaths []string
	var thumbPaths []string
	for rows.Next() {
		var filePath, thumbPath string
		if err := rows.Scan(&filePath, &thumbPath); err == nil {
			filePaths = append(filePaths, filePath)
			thumbPaths = append(thumbPaths, thumbPath)
		}
	}
	rows.Close()

	// Delete photo files and thumbnails from disk
	for _, fp := range filePaths {
		_ = os.Remove(fp)
	}
	for _, tp := range thumbPaths {
		_ = os.Remove(tp)
	}

	// Optionally, remove empty directories (best effort)
	if len(filePaths) > 0 {
		dir := filepath.Dir(filePaths[0])
		_ = os.RemoveAll(filepath.Join(dir, "thumbnails"))
		_ = os.Remove(dir)
	}

	// Delete from DB (photos, tags, colors, etc.)
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction: " + err.Error()})
		return
	}
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM photo_tags WHERE photo_path IN (SELECT file_path FROM photos WHERE request_id = ?)", galleryID)
	_, _ = tx.Exec("DELETE FROM photo_colors WHERE photo_path IN (SELECT file_path FROM photos WHERE request_id = ?)", galleryID)
	_, _ = tx.Exec("DELETE FROM photos WHERE request_id = ?", galleryID)
	_, _ = tx.Exec("DELETE FROM requests WHERE id = ?", galleryID)

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Gallery and all photos deleted"})
}

func deletePhoto(c *gin.Context) {
	photoIDStr := c.Param("id")
	photoID, err := strconv.Atoi(photoIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid photo id"})
		return
	}

	// Get file and thumbnail paths
	var filePath, thumbPath string
	err = db.QueryRow("SELECT file_path, thumbnail_path FROM photos WHERE id = ?", photoID).Scan(&filePath, &thumbPath)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "Photo not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query photo: " + err.Error()})
		return
	}

	// Delete files from disk
	_ = os.Remove(filePath)
	_ = os.Remove(thumbPath)

	// Delete from DB (tags, colors, photo)
	tx, err := db.Begin()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction: " + err.Error()})
		return
	}
	defer tx.Rollback()

	_, _ = tx.Exec("DELETE FROM photo_tags WHERE photo_path = ?", filePath)
	_, _ = tx.Exec("DELETE FROM photo_colors WHERE photo_path = ?", filePath)
	_, _ = tx.Exec("DELETE FROM photos WHERE id = ?", photoID)

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Photo deleted"})
}

func assignPersonToGallery(c *gin.Context) {
	log.Printf("Assigning person to gallery")
	galleryIDStr := c.Param("id")
	galleryID, err := strconv.Atoi(galleryIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid gallery id"})
		return
	}

	var req struct {
		PersonID int `json:"personId"`
	}
	log.Printf("Assigning person to gallery %d", galleryID)
	if err := c.ShouldBindJSON(&req); err != nil || req.PersonID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing or invalid personId"})
		return
	}

	// Get all photo file paths for this gallery
	rows, err := db.Query("SELECT file_path FROM photos WHERE request_id = ?", galleryID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query photos: " + err.Error()})
		return
	}
	defer rows.Close()

	log.Printf("Assigning person %d to gallery %d", req.PersonID, galleryID)

	count := 0
	for rows.Next() {
		var filePath string
		if err := rows.Scan(&filePath); err == nil {
			_, err := db.Exec(
				"INSERT OR IGNORE INTO photo_tags (photo_path, person_id) VALUES (?, ?)",
				filePath, req.PersonID,
			)
			if err == nil {
				count++
			} else {
				log.Printf("Failed to tag photo %s: %v", filePath, err)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      "Person assigned to gallery",
		"photosTagged": count,
	})
}

type SimilarityFeedback struct {
	TargetPhotoID int  `json:"targetPhotoId"`
	IsSimilar     bool `json:"isSimilar"`
}

func favoritePhoto(c *gin.Context) {
	photoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid photo ID"})
		return
	}

	_, err = db.Exec("INSERT OR IGNORE INTO favorites (photo_id) VALUES (?)", photoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to favorite photo"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Photo favorited"})
}

func unfavoritePhoto(c *gin.Context) {
	photoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid photo ID"})
		return
	}

	result, err := db.Exec("DELETE FROM favorites WHERE photo_id = ?", photoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unfavorite photo"})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Photo was not favorited"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Photo unfavorited"})
}

func provideSimilarityFeedback(c *gin.Context) {
	sourcePhotoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid source photo ID"})
		return
	}

	var feedback SimilarityFeedback
	if err := c.ShouldBindJSON(&feedback); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// Store feedback in database
	_, err = db.Exec(`
        INSERT INTO similarity_feedback (source_photo_id, target_photo_id, is_similar) 
        VALUES (?, ?, ?)
        ON CONFLICT (source_photo_id, target_photo_id) 
        DO UPDATE SET is_similar = excluded.is_similar`,
		sourcePhotoID, feedback.TargetPhotoID, feedback.IsSimilar)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store feedback"})
		return
	}

	// Retrain model with all feedback
	go func() {
		if err := retrainModel(); err != nil {
			log.Printf("Error retraining model: %v", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"message": "Feedback recorded and model training started"})
}

func extractImageFeatures(img image.Image) []float64 {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Simple feature extraction - average color values in a 4x4 grid
	features := make([]float64, 48) // 4x4x3 (RGB)

	blockWidth := width / 4
	blockHeight := height / 4

	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			var r, g, b float64
			count := 0

			for x := i * blockWidth; x < (i+1)*blockWidth; x++ {
				for y := j * blockHeight; y < (j+1)*blockHeight; y++ {
					pr, pg, pb, _ := img.At(x, y).RGBA()
					r += float64(pr) / 65535.0
					g += float64(pg) / 65535.0
					b += float64(pb) / 65535.0
					count++
				}
			}

			idx := (i*4 + j) * 3
			features[idx] = r / float64(count)
			features[idx+1] = g / float64(count)
			features[idx+2] = b / float64(count)
		}
	}

	return features
}

func retrainModel() error {
	// Get all feedback pairs
	rows, err := db.Query(`
        SELECT sf.source_photo_id, sf.target_photo_id, sf.is_similar,
               s.file_path as source_path, t.file_path as target_path
        FROM similarity_feedback sf
        JOIN photos s ON sf.source_photo_id = s.id
        JOIN photos t ON sf.target_photo_id = t.id
    `)
	if err != nil {
		return err
	}
	defer rows.Close()

	var features [][]float64
	var similarities []float64

	for rows.Next() {
		var sourcePath, targetPath string
		var isSimilar bool
		var sourceID, targetID int

		if err := rows.Scan(&sourceID, &targetID, &isSimilar, &sourcePath, &targetPath); err != nil {
			continue
		}

		// Extract features from both images
		sourceImg, err := imaging.Open(sourcePath)
		if err != nil {
			continue
		}
		targetImg, err := imaging.Open(targetPath)
		if err != nil {
			continue
		}

		sourceFeatures := extractImageFeatures(sourceImg)
		targetFeatures := extractImageFeatures(targetImg)

		// Calculate feature difference
		diffFeatures := make([]float64, len(sourceFeatures))
		for i := range sourceFeatures {
			diffFeatures[i] = sourceFeatures[i] - targetFeatures[i]
		}

		features = append(features, diffFeatures)
		if isSimilar {
			similarities = append(similarities, 1.0)
		} else {
			similarities = append(similarities, 0.0)
		}
	}

	if len(features) == 0 {
		return nil
	}

	// Train model
	if similarityModel == nil {
		similarityModel = similarity.NewSimilarityModel(len(features[0]))
	}

	return similarityModel.Train(features, similarities)
}

func getSimilarPhotos(c *gin.Context) {
	photoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid photo ID"})
		return
	}

	// Get source photo features
	sourceImg, err := imaging.Open("") // We'll need to get the file path first
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load source image"})
		return
	}

	// Get file path for source photo
	var sourcePath string
	err = db.QueryRow("SELECT file_path FROM photos WHERE id = ?", photoID).Scan(&sourcePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find source photo"})
		return
	}

	sourceImg, err = imaging.Open(sourcePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load source image"})
		return
	}

	sourceFeatures := extractImageFeatures(sourceImg)

	// Get all other photos and calculate similarity using the trained model
	rows, err := db.Query(`
        SELECT p.id, p.file_path, p.thumbnail_path, p.created_at 
        FROM photos p 
        WHERE p.id != ?`, photoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query photos"})
		return
	}
	defer rows.Close()

	type photoWithScore struct {
		PhotoWithTagsAndColors
		Score float64
	}

	var results []photoWithScore
	for rows.Next() {
		var p photoWithScore
		if err := rows.Scan(&p.Id, &p.Path, &p.Thumbnail, &p.CreatedAt); err != nil {
			continue
		}

		targetImg, err := imaging.Open(p.Path)
		if err != nil {
			continue
		}

		targetFeatures := extractImageFeatures(targetImg)
		diffFeatures := make([]float64, len(sourceFeatures))
		for i := range sourceFeatures {
			diffFeatures[i] = sourceFeatures[i] - targetFeatures[i]
		}

		if similarityModel != nil {
			p.Score = similarityModel.Predict(diffFeatures)
			results = append(results, p)
		}
	}

	// Sort by similarity score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Take top 20
	if len(results) > 20 {
		results = results[:20]
	}

	// Convert to regular photos for response
	photos := make([]PhotoWithTagsAndColors, len(results))
	for i, r := range results {
		photos[i] = r.PhotoWithTagsAndColors
	}

	c.JSON(http.StatusOK, photos)
}

func getFeedbackCandidates(c *gin.Context) {
	photoID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid photo ID"})
		return
	}

	// First, find photos that have NOT been labeled for similarity with this photo
	rows, err := db.Query(`
        SELECT p.id, p.request_id, p.url, p.file_path, p.thumbnail_path, p.created_at
        FROM photos p
        WHERE p.id != ?
          AND NOT EXISTS (
            SELECT 1 FROM similarity_feedback sf
            WHERE sf.source_photo_id = ? AND sf.target_photo_id = p.id
          )
        ORDER BY RANDOM()
        LIMIT 10
    `, photoID, photoID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query unlabeled photos"})
		return
	}
	defer rows.Close()

	var photos []PhotoWithTagsAndColors
	for rows.Next() {
		var p PhotoWithTagsAndColors
		if err := rows.Scan(&p.Id, &p.RequestID, &p.URL, &p.Path, &p.Thumbnail, &p.CreatedAt); err != nil {
			continue
		}
		photos = append(photos, p)
	}

	// If there are no unlabeled photos, just return random photos
	if len(photos) == 0 {
		rows, err := db.Query(`
            SELECT id, request_id, url, file_path, thumbnail_path, created_at
            FROM photos
            WHERE id != ?
            ORDER BY RANDOM()
            LIMIT 10
        `, photoID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query random photos"})
			return
		}
		defer rows.Close()
		for rows.Next() {
			var p PhotoWithTagsAndColors
			if err := rows.Scan(&p.Id, &p.RequestID, &p.URL, &p.Path, &p.Thumbnail, &p.CreatedAt); err != nil {
				continue
			}
			photos = append(photos, p)
		}
	}

	c.JSON(http.StatusOK, photos)
}

type PhotoWithTagsAndColors struct {
	Id        int      `json:"id"`
	RequestID int      `json:"RequestId"`
	URL       string   `json:"URL"`
	Path      string   `json:"Path"`
	Thumbnail string   `json:"Thumbnail"`
	CreatedAt string   `json:"CreatedAt"`
	Tags      []string `json:"Tags"`
	Colors    []string `json:"Colors"`
}
