package main

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

func RipImageBam(src string) (string, error) {
	fmt.Printf("Starting RipImageBam for %s\n", src)
	c := newCollector()
	cookieJar, _ := cookiejar.New(nil)
	cookies := []*http.Cookie{
		{Name: "nsfw_inter", Value: "1", Path: "/", Domain: "imagebam.com"},
		{Name: "expires", Value: time.Now().AddDate(0, 0, 1).String(), Path: "/", Domain: "imagebam.com"},
	}
	targetURL, _ := url.Parse("https://imagebam.com")
	cookieJar.SetCookies(targetURL, cookies)
	c.SetCookieJar(cookieJar)

	c.OnResponse(func(r *colly.Response) {
		fmt.Printf("RipImageBam response for %s: Status %d\n", r.Request.URL.String(), r.StatusCode)
	})

	var imageURL string
	c.OnHTML("img.main-image", func(e *colly.HTMLElement) {
		imageURL = e.Attr("src")
		fmt.Printf("Extracted ImageBam URL: %s\n", imageURL)
	})

	if err := c.Visit(src); err != nil {
		return "", fmt.Errorf("visiting ImageBam %s: %v", src, err)
	}
	return imageURL, nil
}

func RipImageBox(src string) (string, error) {
	fmt.Printf("Starting RipImageBox for %s\n", src)
	c := newCollector()
	c.OnResponse(func(r *colly.Response) {
		fmt.Printf("RipImageBox response for %s: Status %d\n", r.Request.URL.String(), r.StatusCode)
	})

	var imageURL string
	c.OnHTML("#img", func(e *colly.HTMLElement) {
		imageURL = e.Attr("src")
		fmt.Printf("Extracted ImgBox URL: %s\n", imageURL)
	})
	if err := c.Visit(src); err != nil {
		return "", fmt.Errorf("visiting ImgBox %s: %v", src, err)
	}
	return imageURL, nil
}

func RipPostImages(src string) (string, error) {
	fmt.Printf("RipPostImages returning direct URL: %s\n", src)
	return src, nil
}

func RipViprIm(src string) (string, error) {
	fmt.Printf("Starting RipViprIm for %s\n", src)
	imageURL := strings.ReplaceAll(src, "/th", "/i")
	fmt.Printf("Transformed Vipr.im URL: %s\n", imageURL)
	return imageURL, nil
}

func RipAcidImg(src string) (string, error) {
	fmt.Printf("Starting RipAcidImg for %s\n", src)
	imageURL := strings.ReplaceAll(src, "t.", "i.")
	imageURL = strings.ReplaceAll(imageURL, "/t", "/i")
	fmt.Printf("Transformed AcidImg URL: %s\n", imageURL)
	return imageURL, nil
}

func RipPixHost(src string) (string, error) {
	fmt.Printf("Starting RipPixHost for %s\n", src)
	imageURL := strings.ReplaceAll(src, "/thumbs", "/images")
	imageURL = strings.ReplaceAll(imageURL, "https://t", "https://img")
	fmt.Printf("Transformed PixHost URL: %s\n", imageURL)
	return imageURL, nil
}

func RipImx(src string) (string, error) {
	fmt.Printf("Starting RipImx for %s\n", src)
	imageURL := strings.ReplaceAll(src, "u/t", "u/i")
	fmt.Printf("Transformed Imx.to URL: %s\n", imageURL)
	return imageURL, nil
}

func RipTurboImg(src string) (string, error) {
	fmt.Printf("Starting RipTurboImg for %s\n", src)
	c := newCollector()
	c.OnResponse(func(r *colly.Response) {
		fmt.Printf("RipTurboImg response for %s: Status %d\n", r.Request.URL.String(), r.StatusCode)
	})

	var imageURL string
	c.OnHTML("#uImageCont img", func(e *colly.HTMLElement) {
		imageURL = e.Attr("src")
		fmt.Printf("Extracted TurboImageHost URL: %s\n", imageURL)
	})
	if err := c.Visit(src); err != nil {
		return "", fmt.Errorf("visiting TurboImageHost %s: %v", src, err)
	}
	return imageURL, nil
}
