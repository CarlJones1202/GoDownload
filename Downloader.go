package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/disintegration/imaging"
)

func DownloadGallery(requestURL, targetUrl, title string) error {
	fmt.Printf("Starting DownloadGallery for %s (title: %s)\n", targetUrl, title)
	_, _, err := processGalleryPage(requestURL, targetUrl, title, nil)
	return err
}

// Helper to process a single page and return new post IDs, and whether to stop
func processGalleryPage(requestURL, targetUrl, title string, processedPosts map[string]bool) ([]string, bool, error) {
	fmt.Printf("Starting processGalleryPage for %s (title: %s)\n", targetUrl, title)

	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				MaxVersion: tls.VersionTLS13,
			},
			ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			fmt.Printf("Redirecting to %s\n", req.URL.String())
			return nil
		},
	}
	req, err := http.NewRequest("GET", targetUrl, nil)
	if err != nil {
		return nil, false, fmt.Errorf("creating request for %s: %v", targetUrl, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:91.0) Gecko/20100101 Firefox/91.0")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	cookies := []*http.Cookie{
		{Name: "__ddg1_", Value: "vS9u2qlH1LdLLfFm3ku8"},
		{Name: "vg_sessionhash", Value: "c89743f0b493d6cbb7768a469c090af1"},
		{Name: "vg_lastvisit", Value: "1741979231"},
		{Name: "vg_lastactivity", Value: "0"},
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}

	maxRetries := 3
	var resp *http.Response
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("Attempt %d of %d to fetch %s\n", attempt, maxRetries, targetUrl)
		resp, err = client.Do(req)
		if err != nil {
			fmt.Printf("Request failed: %v\n", err)
			if attempt == maxRetries {
				return nil, false, fmt.Errorf("fetching %s after %d attempts: %v", targetUrl, maxRetries, err)
			}
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}
	if resp == nil {
		return nil, false, fmt.Errorf("no response received for %s after %d attempts", targetUrl, maxRetries)
	}
	defer resp.Body.Close()

	fmt.Printf("HTTP response for %s: Status %d\n", targetUrl, resp.StatusCode)
	if resp.StatusCode != 200 {
		fmt.Printf("Non-200 status for %s: %d\n", targetUrl, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading response body: %v\n", err)
		}
		if err := os.WriteFile("response.html", body, 0644); err != nil {
			fmt.Printf("Error saving response body: %v\n", err)
		} else {
			fmt.Println("Saved response body to response.html")
		}
		return nil, false, fmt.Errorf("unexpected status %d for %s", resp.StatusCode, targetUrl)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("reading response body: %v", err)
	}
	if strings.Contains(string(body), "DDoS-Guard") || strings.Contains(string(body), "Checking your browser") {
		fmt.Println("DDoS-Guard protection detected. Please update cookies from your browser.")
		if err := os.WriteFile("response.html", body, 0644); err != nil {
			fmt.Printf("Error saving DDoS-Guard response: %v\n", err)
		} else {
			fmt.Println("Saved DDoS-Guard response to response.html")
		}
		return nil, false, fmt.Errorf("blocked by DDoS-Guard; update cookies and retry")
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, false, fmt.Errorf("parsing HTML from %s: %v", targetUrl, err)
	}

	// Determine the main gallery folder (strip /pageN and #post)
	mainUrl := targetUrl
	if idx := strings.Index(mainUrl, "/page"); idx != -1 {
		mainUrl = mainUrl[:idx]
	}
	if idx := strings.Index(mainUrl, "#"); idx != -1 {
		mainUrl = mainUrl[:idx]
	}
	u, err := url.Parse(mainUrl)
	if err != nil {
		return nil, false, fmt.Errorf("parsing URL %s: %v", mainUrl, err)
	}
	u.RawQuery = ""
	mainGalleryDir := downloadDir + "/" + sanitizeFolderName(path.Base(u.Path))
	if title != "" {
		mainGalleryDir += "-" + title
	}

	// Get post ID for subfolder
	postId := ""
	if strings.Contains(targetUrl, "#post") {
		split := strings.Split(targetUrl, "#post")
		postId = "post" + split[len(split)-1]
	}
	if postId == "" {
		// fallback: try to find the first post_message_ div
		doc.Find("div[id^='post_message_']").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			id, exists := s.Attr("id")
			if exists {
				postId = id
				return false // break
			}
			return true
		})
	}
	if postId == "" {
		return nil, false, fmt.Errorf("could not determine post ID for %s", targetUrl)
	}

	// Use subfolder for this post
	directory := fmt.Sprintf("%s/%s", mainGalleryDir, postId)
	fmt.Printf("Creating directory: %s\n", directory)
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		return nil, false, fmt.Errorf("creating directory %s: %v", directory, err)
	}
	thumbnailDir := directory + "/thumbnails"
	if err := os.MkdirAll(thumbnailDir, os.ModePerm); err != nil {
		return nil, false, fmt.Errorf("creating thumbnail directory %s: %v", thumbnailDir, err)
	}
	fmt.Printf("Directory %s and thumbnail dir %s created or already exist\n", directory, thumbnailDir)

	var newPostIDs []string
	done := false

	isFirstMatch := true
	doc.Find(fmt.Sprintf("div[id^='%s']", postId)).Each(func(_ int, s *goquery.Selection) {
		if !isFirstMatch {
			fmt.Println("Skipping additional matches for this post ID")
			return
		}
		// Get the actual post ID from the div
		divID, exists := s.Attr("id")
		if exists {
			if processedPosts != nil && processedPosts[divID] {
				fmt.Printf("Already processed post %s, stopping.\n", divID)
				done = true
				return
			}
			newPostIDs = append(newPostIDs, divID)
		}
		fmt.Printf("Found matching div for %s, parsing images\n", postId)
		count := s.Find("a img").Length()
		fmt.Printf("Detected %d potential image links\n", count)
		s.Find("a img").Each(func(i int, img *goquery.Selection) {
			if img.AttrOr("alt", "") == "View Post" {
				fmt.Printf("Skipping element %d: alt='View Post'\n", i)
				return
			}
			a := img.Parent()
			src, exists := a.Attr("href")
			if !exists {
				fmt.Printf("Element %d: No href found\n", i)
				return
			}
			fmt.Printf("Element %d: Found link %s\n", i, src)
			var imageURL string
			switch {
			case strings.Contains(src, "imagebam"):
				fmt.Println("Ripping from ImageBam")
				imageURL, _ = RipImageBam(src)
			case strings.Contains(src, "imgbox"):
				fmt.Println("Ripping from ImgBox")
				imageURL, _ = RipImageBox(src)
			case strings.Contains(src, "imx.to"):
				fmt.Println("Ripping from Imx.to")
				imageURL, _ = RipImx(img.AttrOr("src", ""))
			case strings.Contains(src, "turboimagehost"):
				fmt.Println("Ripping from TurboImageHost")
				imageURL, _ = RipTurboImg(src)
			case strings.Contains(src, "vipr.im"):
				fmt.Println("Ripping from Vipr.im")
				imageURL, _ = RipViprIm(img.AttrOr("src", ""))
			case strings.Contains(src, "pixhost"):
				fmt.Println("Ripping from PixHost")
				imageURL, _ = RipPixHost(img.AttrOr("src", ""))
			case strings.Contains(src, "acidimg"):
				fmt.Println("Ripping from AcidImg")
				imageURL, _ = RipAcidImg(img.AttrOr("src", ""))
			case strings.Contains(src, "postimages.org"):
				fmt.Println("Ripping from PostImages")
				imageURL, _ = RipPostImages(src)
			case strings.Contains(src, "pixxxels.cc") || strings.Contains(src, "freeimage.us"):
				fmt.Printf("Skipping unsupported host: %s\n", src)
				return
			default:
				fmt.Printf("Unknown image source %s on %s\n", src, targetUrl)
				return
			}
			if imageURL != "" {
				filename := path.Base(imageURL)
				filepath := fmt.Sprintf("%s/%s", directory, filename)
				thumbnailPath := fmt.Sprintf("%s/thumb_%s", thumbnailDir, filename)
				if _, err := os.Stat(filepath); os.IsNotExist(err) {
					if err := DownloadFile(imageURL, filepath); err != nil {
						fmt.Printf("Error downloading %s: %v\n", imageURL, err)
					} else {
						if err := generateThumbnail(filepath, thumbnailPath); err != nil {
							fmt.Printf("Error generating thumbnail: %v\n", err)
						} else {
							if err := storePhoto(requestURL, imageURL, filepath, thumbnailPath); err != nil {
								fmt.Printf("Failed to store photo: %v\n", err)
							}
						}
					}
				}
			}
		})
		isFirstMatch = false
	})

	fmt.Printf("Completed processing for %s\n", targetUrl)
	return newPostIDs, done, nil
}

func generateThumbnail(srcPath, destPath string) error {
	img, err := imaging.Open(srcPath)
	if err != nil {
		return fmt.Errorf("opening image %s: %v", srcPath, err)
	}

	// Resize to width 200, maintaining aspect ratio
	thumb := imaging.Resize(img, 200, 0, imaging.Lanczos)
	if err := imaging.Save(thumb, destPath); err != nil {
		return fmt.Errorf("saving thumbnail %s: %v", destPath, err)
	}
	return nil
}
