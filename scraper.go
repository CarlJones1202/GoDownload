package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

func ListAllPosts(targetUrl string) ([]string, error) {
	fmt.Printf("Starting ListAllPosts for %s\n", targetUrl)
	targetUrl = strings.ReplaceAll(targetUrl, "[range]", "")
	if strings.Contains(targetUrl, "#post") {
		fmt.Printf("Single post detected: %s\n", targetUrl)
		return []string{targetUrl}, nil
	}

	if strings.Contains(targetUrl, "/page") {
		targetUrl = strings.Split(targetUrl, "/page")[0]
	}
	targetUrl += "/page"
	fmt.Printf("Base pagination URL: %s\n", targetUrl)

	postURLs := make(map[string]string)
	page := 1
	c := newCollector()

	c.OnHTML("[id^='post_message_']", func(e *colly.HTMLElement) {
		postId := strings.ReplaceAll(e.Attr("id"), "post_message_", "")
		currentPage := fmt.Sprintf("%s%d", targetUrl, page)
		postURLs[postId] = currentPage + "#post" + postId
		fmt.Printf("Found post ID %s on page %d\n", postId, page)
	})

	for {
		currentPage := fmt.Sprintf("%s%d", targetUrl, page)
		fmt.Printf("Visiting page %d: %s\n", page, currentPage)
		if err := c.Visit(currentPage); err != nil {
			fmt.Printf("Error visiting %s: %v\n", currentPage, err)
			break
		}
		if len(postURLs) == 0 {
			fmt.Printf("No posts found on page %d, stopping\n", page)
			break
		}
		time.Sleep(1 * time.Second)
		prevCount := len(postURLs)
		page++
		fmt.Printf("Checking next page %d\n", page)
		if err := c.Visit(fmt.Sprintf("%s%d", targetUrl, page)); err != nil {
			fmt.Printf("Error on page %d: %v, stopping\n", page, err)
			break
		}
		if len(postURLs) == prevCount {
			fmt.Printf("No new posts on page %d, stopping\n", page)
			break
		}
	}

	var urls []string
	for postId, url := range postURLs {
		fmt.Printf("Collected post %s: %s\n", postId, url)
		urls = append(urls, url)
	}
	fmt.Printf("Total posts collected: %d\n", len(urls))
	return urls, nil
}
