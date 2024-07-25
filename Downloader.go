package main

import (
	"fmt"
	"github.com/gocolly/colly/v2"
	"io"
	"net/http"
	url "net/url"
	"os"
	"path"
	"strings"
)

func main() {
	targetUrl := []string{}

	for _, url := range targetUrl {
		DownloadGallery(url)
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

	directory := fmt.Sprintf("C:\\Downloads\\%s\\", path.Base(u.Path))

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

	c.Visit(targetUrl)
	fmt.Println("Downloaded: " + targetUrl)
}

/*
if (src.Contains("thumbs"))
{
	var url = input.Data.GetSrc().Replace("thumbs", "images").Replace("_t", "_o");
	yield return new DownloadImageDataPackage(url);
}

var handler = new HttpClientHandler();
handler.AllowAutoRedirect = true;
var web = new HttpClient(handler);

var resp = await web.GetAsync(src);
var t = resp.RequestMessage.RequestUri;

var redirected_url = resp.RequestMessage.RequestUri.ToString().Replace("thumbs", "images").Replace("_t", "_o");

*/

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
	defer out.Close()

	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return err
	}

	return nil
}
