package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

type Person struct {
	ID           int      `db:"id"`
	Name         string   `db:"name" json:"name"`
	PhotoCount   int      `json:"photoCount"`
	GalleryCount int      `json:"galleryCount"`
	Aliases      []string `db:"aliases" json:"aliases"` // Changed to []string
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

	matchedPersonIDs, err := extractPersonIDsFromURL(galleryURL)
	if err != nil {
		return fmt.Errorf("extracting person IDs from URL %s: %v", galleryURL, err)
	}
	if len(matchedPersonIDs) == 0 {
		log.Printf("No matching people found in URL %s", galleryURL)
		return nil
	}

	for _, personID := range matchedPersonIDs {
		_, err := db.Exec(`
            INSERT OR IGNORE INTO photo_tags (photo_path, person_id) 
            VALUES (?, ?)`, photoPath, personID)
		if err != nil {
			return fmt.Errorf("tagging photo %s with person %d: %v", photoPath, personID, err)
		}
		log.Printf("Tagged %s with person ID %d from gallery %s", photoPath, personID, galleryURL)
	}

	return nil
}

func extractPersonIDsFromURL(url string) ([]int, error) {
	rows, err := db.Query("SELECT id, name, aliases FROM people")
	if err != nil {
		return nil, fmt.Errorf("fetching people: %v", err)
	}
	defer rows.Close()

	lowerURL := strings.ToLower(url)
	var matchedPersonIDs []int
	for rows.Next() {
		var id int
		var personName string
		var aliasesJSON sql.NullString
		if err := rows.Scan(&id, &personName, &aliasesJSON); err != nil {
			log.Printf("Failed to scan person row: %v", err)
			continue
		}

		var aliases []string
		if aliasesJSON.Valid && aliasesJSON.String != "" && aliasesJSON.String != `""` {
			if err := json.Unmarshal([]byte(aliasesJSON.String), &aliases); err != nil {
				log.Printf("Failed to unmarshal aliases '%s' for person %s (ID: %d): %v", aliasesJSON.String, personName, id, err)
				aliases = []string{}
			}
		} else {
			aliases = []string{}
			log.Printf("Aliases for %s (ID: %d) is empty, NULL, or quotes-only: '%s'", personName, id, aliasesJSON.String)
		}

		lowerName := strings.ToLower(personName)
		nameVariants := generateNameVariants(lowerName)
		for _, variant := range nameVariants {
			if strings.Contains(lowerURL, variant) {
				matchedPersonIDs = append(matchedPersonIDs, id)
				log.Printf("Matched person %s (ID: %d) with variant '%s' in URL", personName, id, variant)
				break
			}
		}

		for _, alias := range aliases {
			lowerAlias := strings.ToLower(alias)
			aliasVariants := generateNameVariants(lowerAlias)
			for _, variant := range aliasVariants {
				if strings.Contains(lowerURL, variant) {
					matchedPersonIDs = append(matchedPersonIDs, id)
					log.Printf("Matched alias %s for person ID %d with variant '%s' in URL", alias, id, variant)
					break
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating people rows: %v", err)
	}

	return matchedPersonIDs, nil
}

func generateNameVariants(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{}
	}

	noSpaces := strings.ReplaceAll(name, " ", "")
	variants := []string{
		name,
		noSpaces,
		strings.ReplaceAll(name, " ", "-"),
		strings.ReplaceAll(name, " ", "_"),
		strings.ReplaceAll(name, " ", "."),
	}

	uniqueVariants := make(map[string]bool)
	var result []string
	for _, v := range variants {
		if !uniqueVariants[v] {
			uniqueVariants[v] = true
			result = append(result, v)
		}
	}

	return result
}
