package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
)

func sanitizeFolderName(name string) string {
	fmt.Printf("Sanitizing folder name: %s\n", name)
	invalidCharsRegex := regexp.MustCompile("[<>:\"/\\|?*]")
	sanitizedName := invalidCharsRegex.ReplaceAllString(name, "_")
	sanitizedName = strings.Trim(sanitizedName, "_")
	fmt.Printf("Sanitized name: %s\n", sanitizedName)
	return sanitizedName
}

func DownloadFile(url, filepath string) error {
	fmt.Printf("Starting download of %s to %s\n", url, filepath)
	out, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("creating file %s: %v", filepath, err)
	}
	defer out.Close()
	fmt.Printf("Created file %s\n", filepath)

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
		return fmt.Errorf("writing to %s: %v", filepath, err)
	}
	fmt.Printf("Completed writing to %s\n", filepath)
	return nil
}
