package main

import (
	"fmt"
	"github.com/gocolly/colly/v2"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

func main() {
	targetUrls := []string{}

	for _, targetUrl := range targetUrls {
		DownloadGallery(targetUrl)
	}
}
func DownloadGallery(targetUrl string) {
	c := colly.NewCollector()
	u, _ := url.Parse(targetUrl)
	u.RawQuery = ""

	postId := "post_message_"
	if strings.Contains(targetUrl, "#post") {
		split := strings.Split(targetUrl, "#post")
		postId += split[len(split)-1]
	}

	directory := fmt.Sprintf("C:\\Users\\carlj\\Downloads\\%s\\", path.Base(u.Path))

	if _, err := os.Stat(directory); err == nil {
		fmt.Println("Already downloaded: " + targetUrl)
		return
	}

	_ = os.Mkdir(directory, os.ModePerm)
	isFirstMatch := true
	// Find and visit all links
	c.OnHTML(fmt.Sprintf("div[id^='%s']", postId), func(e *colly.HTMLElement) {
		if isFirstMatch {
			e.ForEach("a img", func(i int, element *colly.HTMLElement) {
				a := element.DOM.Parent()
				src, _ := a.Attr("href")
				switch {
				case strings.Contains(src, "imagebam"):
					src = RipImageBam(src)
				case strings.Contains(src, "imgbox"):
					src = RipImageBox(element.Attr("src"))
				case strings.Contains(src, "imx.to"):
					src = RipImx(element.Attr("src"))
				case strings.Contains(src, "turboimagehost"):
					src = RipTurboImg(src)
				case strings.Contains(src, "vipr.im"):
					src = RipViprIm(element.Attr("src"))
				case strings.Contains(src, "pixhost"):
					src = RipPixHost(element.Attr("src"))
				case strings.Contains(src, "acidimg"):
					src = RipAcidImg(element.Attr("src"))
				default:
					panic(fmt.Sprintf("Unknown image source %v", src))
				}

				filename := path.Base(src)

				err := DownloadFile(src, fmt.Sprintf("%s\\%s", directory, filename))
				if err != nil {
					panic(err)
				}
			})
			isFirstMatch = false
		}
	})

	_ = c.Visit(targetUrl)
	fmt.Println("Downloaded: " + targetUrl)
}

func RipImageBam(src string) string {
	c := colly.NewCollector()
	cookieJar, _ := cookiejar.New(nil)

	cookies := make([]*http.Cookie, 2)
	cookies[0] = &http.Cookie{
		Name:   "nsfw_inter",
		Value:  "1",
		Path:   "/",
		Domain: "imagebam.com",
	}
	cookies[1] = &http.Cookie{
		Name:   "expires",
		Value:  time.Now().AddDate(0, 0, 1).String(),
		Path:   "/",
		Domain: "imagebam.com",
	}

	targetUrl, _ := url.Parse("https://imagebam.com")
	cookieJar.SetCookies(targetUrl, cookies)
	c.SetCookieJar(cookieJar)

	c.OnHTML("img.main-image", func(e *colly.HTMLElement) {
		src = e.Attr("src")
	})

	err := c.Visit(src)
	if err != nil {
		panic(err)
	}

	return src
}

func RipImageBox(src string) string {
	if strings.Contains(src, "_o") {
		return src
	} else {
		panic("unknown image box path")
	}
}

func RipViprIm(src string) string {
	src = strings.ReplaceAll(src, "/th", "/i")
	return src
}

func RipAcidImg(src string) string {
	src = strings.ReplaceAll(src, "t.", "i.")
	src = strings.ReplaceAll(src, "/t", "/i")
	return src
}

func RipPixHost(src string) string {
	src = strings.ReplaceAll(src, "/thumbs", "/images")
	src = strings.ReplaceAll(src, "https://t", "https://img")
	return src
}

func RipImx(src string) string {
	src = strings.ReplaceAll(src, "u/t", "u/i")
	return src
}

func RipTurboImg(src string) string {
	c := colly.NewCollector()

	c.OnHTML("#uImageCont img", func(e *colly.HTMLElement) {
		src = e.Attr("src")
	})

	err := c.Visit(src)
	if err != nil {
		panic(err)
	}
	return src
}

// DownloadFile will download a url and store it in local filepath.
// It writes to the destination file as it downloads it, without
// loading the entire file into memory.
func DownloadFile(url string, filepath string) error {
	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer func(out *os.File) {
		_ = out.Close()
	}(out)

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
