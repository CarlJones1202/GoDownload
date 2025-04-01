package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

// StashDBPerson represents a performer from StashDB
type StashDBPerson struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

// GraphQLRequest for StashDB queries
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse for StashDB responses
type GraphQLResponse struct {
	Data struct {
		FindPerformers struct {
			Performers []StashDBPerson `json:"performers"`
		} `json:"findPerformers"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// searchStashDBPeople handles searching StashDB for performers by name
func searchStashDBPeople(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Name query parameter is required"})
		return
	}

	apiKey := os.Getenv("STASHDB_API_KEY")
	if apiKey == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Server configuration error: API key missing"})
		return
	}

	// GraphQL query for performer search
	query := `
		query FindPerformers($name: String!) {
			findPerformers(filter: { q: $name, per_page: 10 }) {
				performers {
					id
					name
					aliases
				}
			}
		}`
	variables := map[string]interface{}{
		"name": name,
	}
	payload := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encode request"})
		return
	}

	req, err := http.NewRequest("POST", "https://stashdb.org/graphql", bytes.NewBuffer(reqBody))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create request"})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ApiKey", apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query StashDB: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "StashDB returned status: " + resp.Status})
		return
	}

	var result GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode response"})
		return
	}

	if len(result.Errors) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "GraphQL errors: " + result.Errors[0].Message})
		return
	}

	// Always return an array, even if empty
	c.JSON(http.StatusOK, result.Data.FindPerformers.Performers)
}
