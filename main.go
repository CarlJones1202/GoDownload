package main

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
)

const downloadDir = "downloads"

var (
	urlQueue = make(chan string, 100)
	wg       sync.WaitGroup
)

func main() {
	if err := initDB(); err != nil {
		fmt.Printf("Failed to initialize database: %v\n", err)
		return
	}

	numWorkers := 5
	for i := 0; i < numWorkers; i++ {
		go worker(i)
	}

	r := gin.Default()

	// Add CORS middleware
	r.Use(corsMiddleware())

	// Serve the downloads directory
	r.Static("/files", "./downloads")

	r.POST("/download", queueDownloads)
	r.GET("/photos", listPhotos)

	if err := r.Run(":8080"); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func worker(id int) {
	for url := range urlQueue {
		fmt.Printf("Worker %d processing %s\n", id, url)
		wg.Add(1)
		if err := processURL(url); err != nil {
			fmt.Printf("Worker %d error processing %s: %v\n", id, url, err)
		}
		wg.Done()
	}
}

func queueDownloads(c *gin.Context) {
	var payload struct {
		URLs []string `json:"urls" binding:"required"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload: " + err.Error()})
		return
	}

	for _, url := range payload.URLs {
		if err := storeRequest(url); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store request: " + err.Error()})
			return
		}
		urlQueue <- url
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "URLs queued for processing", "count": len(payload.URLs)})
}

func processURL(url string) error {
	photos, err := DownloadGallery(url, "")
	if err != nil {
		return err
	}
	for _, photo := range photos {
		if err := storePhoto(url, photo.URL, photo.Path); err != nil {
			fmt.Printf("Failed to store photo %s: %v\n", photo.URL, err)
		}
	}
	return nil
}

func listPhotos(c *gin.Context) {
	page := c.DefaultQuery("page", "1")
	perPage := c.DefaultQuery("per_page", "10")

	photos, total, err := getPhotos(page, perPage)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	pp, err := strconv.Atoi(perPage)
	if err != nil || pp < 1 {
		pp = 10
	}

	c.JSON(http.StatusOK, gin.H{
		"photos":      photos,
		"total":       total,
		"page":        page,
		"per_page":    perPage,
		"total_pages": (total + int64(pp) - 1) / int64(pp),
	})
}
