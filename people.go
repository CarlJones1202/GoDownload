package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

type Person struct {
	ID      int    `db:"id"`
	Name    string `db:"name"`
	Aliases string `db:"aliases"` // JSON string in DB
}

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

	// Fetch all existing people
	rows, err := db.Query("SELECT id, name, aliases FROM people")
	if err != nil {
		return fmt.Errorf("fetching people: %v", err)
	}
	defer rows.Close()

	// Look for a match in names or aliases
	var matchedPersonID int
	for rows.Next() {
		var id int
		var personName, aliasesJSON string
		if err := rows.Scan(&id, &personName, &aliasesJSON); err != nil {
			log.Printf("Failed to scan person row: %v", err)
			continue
		}

		var aliases []string
		if err := json.Unmarshal([]byte(aliasesJSON), &aliases); err != nil {
			log.Printf("Failed to unmarshal aliases for %s: %v", personName, err)
			continue
		}

		// Case-insensitive match against name or aliases
		if strings.EqualFold(personName, name) {
			matchedPersonID = id
			break
		}
		for _, alias := range aliases {
			if strings.EqualFold(alias, name) {
				matchedPersonID = id
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating people rows: %v", err)
	}

	if matchedPersonID == 0 {
		log.Printf("No matching person found for name %s", name)
		return nil
	}

	// Tag the photo with the matched person
	_, err = db.Exec(`
        INSERT OR IGNORE INTO photo_tags (photo_path, person_id) 
        VALUES (?, ?)`, photoPath, matchedPersonID)
	if err != nil {
		return fmt.Errorf("tagging photo %s with person %d: %v", photoPath, matchedPersonID, err)
	}

	log.Printf("Tagged %s with person ID %d (name: %s) from gallery %s", photoPath, matchedPersonID, name, galleryURL)
	return nil
}

// extractPersonNameFromURL remains unchanged
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

	// Skip thread ID and prefixes, including date (YYYY-MM-DD or YYYY_MM_DD)
	startIdx := 1
	knownPrefixes := map[string]bool{
		"metart":     true,
		"metart-com": true,
		"studio":     true,
	}
	// Check for date pattern: YYYY-MM-DD (e.g., "2016-01-03") or YYYY_MM_DD (e.g., "2016_01_13")
	if startIdx+2 < len(segments) {
		// Handle YYYY-MM-DD
		if regexp.MustCompile(`^\d{4}$`).MatchString(segments[startIdx]) &&
			regexp.MustCompile(`^\d{2}$`).MatchString(segments[startIdx+1]) &&
			regexp.MustCompile(`^\d{2}$`).MatchString(segments[startIdx+2]) {
			log.Printf("Detected date pattern at index %d: %s-%s-%s", startIdx, segments[startIdx], segments[startIdx+1], segments[startIdx+2])
			startIdx += 3 // Skip past the date
		} else {
			// Handle YYYY_MM_DD in a single segment (e.g., "2016_01_13")
			if regexp.MustCompile(`^\d{4}_\d{2}_\d{2}$`).MatchString(segments[startIdx]) {
				log.Printf("Detected underscore date pattern at index %d: %s", startIdx, segments[startIdx])
				startIdx++ // Skip the single date segment
			}
		}
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

		// Early rejection: any segment with numbers (unless it's the date we skipped)
		if regexp.MustCompile(`\d`).MatchString(segment) {
			log.Printf("Segment %s (index %d) rejected: contains numbers", segment, i)
			break
		}

		// Stop at gallery details, metadata, or date indicators
		if regexp.MustCompile(`^\d+x\d+$`).MatchString(segment) || // e.g., "3456x5184"
			regexp.MustCompile(`^x\d+$`).MatchString(segment) || // e.g., "x120"
			strings.Contains(segment, "(") || // e.g., "(x120)"
			lowerSeg == "pictures" || lowerSeg == "px" || // e.g., "pictures"
			(i > startIdx && regexp.MustCompile(`^[A-Z][a-z]+$`).MatchString(segment) && len(nameParts) > 0) { // e.g., "Qetena" after name
			log.Printf("Stopping at segment %s (index %d)", segment, i)
			break
		}
		nameParts = append(nameParts, segment)
	}

	if len(nameParts) == 0 {
		log.Printf("No name parts found in %s after index %d", url, startIdx)
		return "Unknown"
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
