package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gocolly/colly/v2"
)

func sanitizeFolderName(name string) string {
	fmt.Printf("Sanitizing folder name: %s\n", name)
	invalidCharsRegex := regexp.MustCompile("[<>:\"/\\|?*]")
	sanitizedName := invalidCharsRegex.ReplaceAllString(name, "_")
	sanitizedName = strings.Trim(sanitizedName, "_")
	fmt.Printf("Sanitized name: %s\n", sanitizedName)
	return sanitizedName
}

func DownloadFile(url, path string) error {
	fmt.Printf("Starting download of %s to %s\n", url, path)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %v", dir, err)
	}
	fmt.Printf("Ensured directory %s exists\n", dir)

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating file %s: %v", path, err)
	}
	defer out.Close()
	fmt.Printf("Created file %s\n", path)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("fetching %s: %v", url, err)
	}
	defer resp.Body.Close()
	fmt.Printf("HTTP response for %s: Status %s\n", url, resp.Status)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %s for %s", resp.Status, url)
	}

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("writing to %s: %v", path, err)
	}
	fmt.Printf("Completed writing to %s\n", path)
	return nil
}

func newCollector() *colly.Collector {
	fmt.Println("Creating new Colly collector")
	return colly.NewCollector()
}
