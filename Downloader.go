package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/disintegration/imaging"
	"golang.org/x/net/publicsuffix"
)

var (
	cookieFile = "cookies.json"
	jar        *cookiejar.Jar
)

func init() {
	// Initialize cookie jar
	var err error
	jar, err = cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		fmt.Printf("Failed to initialize cookie jar: %v\n", err)
	}
	loadCookies()
}

// Load cookies from file
func loadCookies() {
	data, err := os.ReadFile(cookieFile)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Printf("Error reading cookie file: %v\n", err)
		}
		return
	}
	var cookies []*http.Cookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		fmt.Printf("Error unmarshaling cookies: %v\n", err)
		return
	}
	u, _ := url.Parse("https://vipergirls.to")
	jar.SetCookies(u, cookies)
}

// Save cookies to file
func saveCookies() {
	u, _ := url.Parse("https://vipergirls.to")
	cookies := jar.Cookies(u)
	data, err := json.Marshal(cookies)
	if err != nil {
		fmt.Printf("Error marshaling cookies: %v\n", err)
		return
	}
	if err := os.WriteFile(cookieFile, data, 0644); err != nil {
		fmt.Printf("Error writing cookie file: %v\n", err)
	}
}

// Refresh cookies from vipergirls.to
func refreshCookies(client *http.Client) error {
	req, err := http.NewRequest("GET", "https://vipergirls.to/", nil)
	if err != nil {
		return fmt.Errorf("creating refresh request: %v", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("refreshing cookies: %v", err)
	}
	defer resp.Body.Close()
	saveCookies()
	fmt.Println("Cookies refreshed from vipergirls.to")
	return nil
}

func DownloadGallery(requestURL, targetUrl, title string) error {
	fmt.Printf("Starting DownloadGallery for %s (title: %s)\n", targetUrl, title)

	client := &http.Client{
		Timeout:   60 * time.Second,
		Jar:       jar,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS13}},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			fmt.Printf("Redirecting to %s\n", req.URL.String())
			return nil
		},
	}

	// Refresh cookies if they're empty or expired
	u, _ := url.Parse("https://vipergirls.to")
	if len(jar.Cookies(u)) == 0 || time.Now().Sub(jar.Cookies(u)[0].Expires) > 0 {
		if err := refreshCookies(client); err != nil {
			return fmt.Errorf("failed to refresh cookies: %v", err)
		}
	}

	req, err := http.NewRequest("GET", targetUrl, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %v", targetUrl, err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Accept-Encoding", "gzip, deflate, br")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", "https://vipergirls.to/")
	req.Header.Set("Upgrade-Insecure-Requests", "1")

	maxRetries := 3
	var resp *http.Response
	for attempt := 1; attempt <= maxRetries; attempt++ {
		fmt.Printf("Attempt %d of %d to fetch %s\n", attempt, maxRetries, targetUrl)
		resp, err = client.Do(req)
		if err != nil {
			fmt.Printf("Request failed: %v\n", err)
			if attempt == maxRetries {
				return fmt.Errorf("fetching %s after %d attempts: %v", targetUrl, maxRetries, err)
			}
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}
	if resp == nil {
		return fmt.Errorf("no response received for %s after %d attempts", targetUrl, maxRetries)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		os.WriteFile("response.html", body, 0644)
		return fmt.Errorf("unexpected status %d for %s", resp.StatusCode, targetUrl)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}
	if strings.Contains(string(body), "DDoS-Guard") || strings.Contains(string(body), "Checking your browser") {
		fmt.Println("DDoS-Guard detected, refreshing cookies")
		if err := refreshCookies(client); err != nil {
			return fmt.Errorf("blocked by DDoS-Guard and failed to refresh cookies: %v", err)
		}
		return DownloadGallery(requestURL, targetUrl, title) // Retry with fresh cookies
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("parsing HTML from %s: %v", targetUrl, err)
	}

	tempTarget := targetUrl
	if strings.Contains(tempTarget, "/page") {
		tempTarget = strings.Split(targetUrl, "/page")[0]
	}
	fmt.Printf("Base target URL: %s\n", tempTarget)

	u, err = url.Parse(tempTarget)
	if err != nil {
		return fmt.Errorf("parsing URL %s: %v", targetUrl, err)
	}
	u.RawQuery = ""
	fmt.Printf("Parsed URL path: %s\n", u.Path)

	postId := "post_message_"
	if strings.Contains(targetUrl, "#post") {
		split := strings.Split(targetUrl, "#post")
		postId += split[len(split)-1]
	}
	fmt.Printf("Targeting post ID: %s\n", postId)

	directory := downloadDir + "/" + sanitizeFolderName(path.Base(u.Path))
	if title != "" {
		directory += "-" + title
	}
	fmt.Printf("Creating directory: %s\n", directory)
	if err := os.MkdirAll(directory, os.ModePerm); err != nil {
		return fmt.Errorf("creating directory %s: %v", directory, err)
	}
	thumbnailDir := directory + "/thumbnails"
	if err := os.MkdirAll(thumbnailDir, os.ModePerm); err != nil {
		return fmt.Errorf("creating thumbnail directory %s: %v", thumbnailDir, err)
	}
	fmt.Printf("Directory %s and thumbnail dir %s created or already exist\n", directory, thumbnailDir)

	isFirstMatch := true
	doc.Find(fmt.Sprintf("div[id^='%s']", postId)).Each(func(_ int, s *goquery.Selection) {
		if !isFirstMatch {
			fmt.Println("Skipping additional matches for this post ID")
			return
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
					} else if err := generateThumbnail(filepath, thumbnailPath); err != nil {
						fmt.Printf("Error generating thumbnail: %v\n", err)
					} else if err := storePhoto(requestURL, imageURL, filepath, thumbnailPath); err != nil {
						fmt.Printf("Failed to store photo: %v\n", err)
					}
				}
			}
		})
		isFirstMatch = false
	})

	fmt.Printf("Completed processing for %s\n", targetUrl)
	return nil
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
