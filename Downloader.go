package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gocolly/colly/v2"
)

func main() {
	targetUrls := []string{
		"https://vipergirls.to/threads/3961811-Serena-Wood-Dualeco-2015-02-17-4324px",
		"https://vipergirls.to/threads/1966325-Serena-Wood-Pink-Serenity-(Jul-28-2016)?p=17111275#post17111275",
		"https://vipergirls.to/threads/3961871-Serena-Wood-Manzana-2017-05-31-5000px",
		"https://vipergirls.to/threads/5518259-Serena-Wood-CAMA-89-Photos-Sep-05-2020",
		"https://vipergirls.to/threads/3767886-Serena-Wood-Madote-x122-5616x3744",
		"https://vipergirls.to/threads/3458675-Serena-Wood-in-Splash-of-Color-x42-2700px-(02-06-2018)",
		"https://vipergirls.to/threads/3421314-Serena-Wood-in-Bright-Eyes-x42-2700px-(01-23-2018)",
		"https://vipergirls.to/threads/6076582-Serena-Wood-Every-Season-118-Photos-May-06-2021",
		"https://vipergirls.to/threads/8775676-Serena-Bright-Girl-4752-Pix-201-Jpg-06-12-2014",
		"https://vipergirls.to/threads/10871507-Serena-Wood-Playful-Bombshell-x77-August-2017?p=183304163",
		"https://vipergirls.to/threads/9346880-Serena-Wood-Fresh-Fruit-(X112)-3648x5472",
		"https://vipergirls.to/threads/4246598-Serena-Wood-Original-01-24-19",
		"https://vipergirls.to/threads/5085660-MetArt-com-Serena-Wood-Filming-(Feb-23-2020)",
		"https://vipergirls.to/threads/4572085-MetArt-com-Serena-Wood-Subtly-Sexy-(Jun-20-2019)",
		"https://vipergirls.to/threads/9357275-Serena-Wood-Mount-(X107)-3648x5472",
		"https://vipergirls.to/threads/9330542-Serena-Wood-Toast-Of-Ma-(X102)-3648x5472",
		"https://vipergirls.to/threads/11508059-Amelia-A-Feeling-Deep-Inside-2-x83-(08-20-24)",
		"https://vipergirls.to/threads/9073055-2023-09-19-Amelia-Rain-Bird-6775-x148",
		"https://vipergirls.to/threads/7767215-2023-02-01-Amelia-Ivory-Tower-6623-x147",
		"https://vipergirls.to/threads/11037819-Serena-Wood-Cliff-Side-x150-(06-21-24)",
		"https://vipergirls.to/threads/3961813-Serena-Wood-Daily-2015-08-15-4324px",
		"https://vipergirls.to/threads/6938232-Serena-Wood-Royal-x107-(Apr-8-2022)",
		"https://vipergirls.to/threads/6856004-Serena-Wood-Mount-107-Photos-Mar-10-2022",
		"https://vipergirls.to/threads/5769885-Serena-Wood-Holiday-Light-120-Photos-Dec-31-2020",
		"https://vipergirls.to/threads/6970527-Serena-Wood-Filming-(x120)?p=105031472",
		"https://vipergirls.to/threads/7349465-Serena-Wood-Fragrant-Dream-108-Photos-Sep-13-2022",
		"https://vipergirls.to/threads/9477792-Serena-Wood-The-Great-Room-(x97)-3648x5472",
		"https://vipergirls.to/threads/6556746-Serena-Wood-Rockies-115-Photos-Nov-24-2021",
		"https://vipergirls.to/threads/6491198-Serena-Wood-Domanta-(123x)-5600px",
		"https://vipergirls.to/threads/6215917-Serena-Wood-Semila-4324-Pix-116-Jpg-22-02-2016",
		"https://vipergirls.to/threads/4926036-Serena-Wood-%E2%80%93-Floral-Dress-120-pictures-6720px-(8-Dec-2019)",
		"https://vipergirls.to/threads/4976984-MetArt-com-Serena-Wood-Cheer-(Dec-24-2019)?p=57605243#post57605243",
		"https://vipergirls.to/threads/4830294-MetArt-com-Serena-Wood-Wild-Dress-(Oct-25-2019)",
		"https://vipergirls.to/threads/5839424-Serena-Wood-Upside-x85",
		"https://vipergirls.to/threads/4181184-MetArt-com-Serena-Wood-Seasons-Greetings-(Dec-24-2018)",
		"https://vipergirls.to/threads/3962198-Serena-Wood-The-Hot-Chair-2015-08-22-5616px",
		"https://vipergirls.to/threads/5161716-MetArt-com-Serena-Wood-Trimmed-(Apr-01-2020)",
		"https://vipergirls.to/threads/3705293-2016_01_13-Serena-Wood-Qetena-(x148)",
		"https://vipergirls.to/threads/4660329-Serena-Wood-%E2%80%93-Braided-119-pictures-5472px-(4-Aug-2019)",
		"https://vipergirls.to/threads/3577896-Serena-Wood-Esena-(X119)-3840x5760",
		"https://vipergirls.to/threads/5937709-February-21-2021-Serena-Wood-Disco-Is-Dead-32-pics-6-MB",
		"https://vipergirls.to/threads/3961150-Serena-Wood-Karemn-(X120)(3744x5616)",
		"https://vipergirls.to/threads/4032138-Serena-Wood-Scenery-amp-Beauty-10-22-18",
		"https://vipergirls.to/threads/3961865-Serena-Wood-Patio-2017-03-31-5000px",
		"https://vipergirls.to/threads/3961814-Serena-Wood-Lacuna-2016-03-03-4324px",
		"https://vipergirls.to/threads/5109983-08-01-17-Serena-Wood-Jateen-117-pics-2-883px-",
		"https://vipergirls.to/threads/4252209-26-01-2019-Serena-Wood-Glass-Of-Wine-124-pics-204-34-MB",
		"https://vipergirls.to/threads/6027973-Serena-Wood-Laid-Back-5616-Pix-120-Jpg-29-06-2015",
		"https://vipergirls.to/threads/6447261-Serena-Wood-Lasena-4324-Pix-119-Jpg-03-05-2016?p=92541324&viewfull=1",
		"https://vipergirls.to/threads/4811946-Serena-Wood-Nirmea-24-07-2014-(120-photos)(3456-X-5184)?p=54051361#post54051361",
		"https://vipergirls.to/threads/1911298-Serena-Wood-in-Pillow-Play-x40-2700px-(07-14-2016)",
		"https://vipergirls.to/threads/4183668-Serena-Wood-EVE-12-26-18",
		"https://vipergirls.to/threads/3854440-MetArt-2015-06-19-Serena-Wood-Narove-(x120)-2848x4288",
		"https://vipergirls.to/threads/2465921-Serena-Wood-Beach-Girl-X84",
		"https://vipergirls.to/threads/4121637-Serena-Wood-Thelly-106-pictures-4324px-(6-Nov-2016)",
		"https://vipergirls.to/threads/10139634-2017-04-21-Serena-Wood-Set-2-(x56)",
		"https://vipergirls.to/threads/3628257-Serena-Wood-Etinda-(X128)-3840x5760",
		"https://vipergirls.to/threads/5115516-03-05-17-Serena-Wood-Alame-2-848px-",
		"https://vipergirls.to/threads/9362498-Serena-Wood-Passion-x65-4500px-Jul-27-2015",
		"https://vipergirls.to/threads/6478526-Serena-Wood-Penso-4324-Pix-55-Jpg-17-08-2016",
		"https://vipergirls.to/threads/3478420-Serena-Wood-in-Shimmering-Sensation-x42-2700px-(02-14-2018)",
		"https://vipergirls.to/threads/3962199-Serena-Wood-Comfort-2015-06-11-5616px",
		"https://vipergirls.to/threads/3961674-Serena-Wood-Suite-2014-09-23-4324px",
		"https://vipergirls.to/threads/3744425-(2015-07-30)-Serena-Wood-Ditiso-(X120)-3840x5760",
		"https://vipergirls.to/threads/5598996-Serena-Wood-Aster-4324-Pix-65-Jpg-30-08-2014",
		"https://vipergirls.to/threads/3935491-Presenting-Serena-Wood-06-22-2014-4324x2883-122-pics-165-MB",
		"https://vipergirls.to/threads/7193257-Serena-Wood-Serena-Wood-2-56-pictures-5616px",
		"https://vipergirls.to/threads/4842190-Serena-Wood-Sylogi-x121-4324x2883",
		"https://vipergirls.to/threads/7400380-Serena-Wood-Classic-Studio-(x130)-1365x2048-Sexy-Photo-Gallery?p=115326041#post115326041",
		"https://vipergirls.to/threads/6279302-Serena-Wood-All-Year-Long-121-Photos-Aug-10-2021",
		"https://vipergirls.to/threads/10862717-MPLStudios-com-Amelia-The-Main-Dish-2-(May-26-2024)",
		"https://vipergirls.to/threads/10713488-MPLStudios-com-Amelia-Cachet-(May-10-2024)",
		"https://vipergirls.to/threads/9565571-MPLStudios-com-Amelia-Night-At-The-Circus-2-(Dec-20-2023)",
		"https://vipergirls.to/threads/10086705-MPLStudios-com-Amelia-Hearts-Are-Wild-(Feb-14-2024)",
		"https://vipergirls.to/threads/1368409-Amelia-Comfort-First-65-pics-cover-4000px-Set-4691-2015-11-10?p=11406546&viewfull=1#post11406546",
		"https://vipergirls.to/threads/10970328-MPLStudios-com-Amelia-Sweltering-Heat-(Jun-09-2024)",
		"https://vipergirls.to/threads/9811064-MPLStudios-com-Amelia-Trickster-(Jan-15-2024)",
		"https://vipergirls.to/threads/9659874-MPLStudios-com-Amelia-The-Main-Dish-(Jan-03-2024)",
		"https://vipergirls.to/threads/9425988-MPLStudios-com-Amelia-The-Move-(Nov-22-2023)",
		"https://vipergirls.to/threads/10618657-MPLStudios-com-Amelia-The-Other-Move-(Apr-26-2024)",
		"https://vipergirls.to/threads/9351669-Amelia-Casual-Style-2-x98-4000px-6750-(04-11-2023)",
		"https://vipergirls.to/threads/4203215-Amelia-The-Night-Shift-x93-4000px-Dec-12-2015",
		"https://vipergirls.to/threads/1368409-Amelia-Comfort-First-65-pics-cover-4000px-Set-4691-2015-11-10",
		"https://vipergirls.to/threads/10614525-MPLStudios-com-Amelia-An-Eye-On-You-2-(Feb-28-2024)",
		"https://vipergirls.to/threads/10614876-MPLStudios-com-Amelia-If-Not-Now-When-(Mar-13-2024)?p=175356633",
		"https://vipergirls.to/threads/9385357-MPLStudios-com-Amelia-Rain-Bird-(Sep-19-2023)",
		"https://vipergirls.to/threads/7332740-MPL-Studios-Model-Sets?p=132779602",
		"https://vipergirls.to/threads/4607601-2016-10-12-Amelia-Afternoon-Delight-(X74)-2668x4000",
		"https://vipergirls.to/threads/7269650-2015-09-06-Amelia-Ivory-Angel-(x67)",
		"https://vipergirls.to/threads/7295693-2016-07-20-Amelia-Sitting-Pretty-x69-4000px",
		"https://vipergirls.to/threads/4232424-17-01-2019-017-MPL-Amelia-Velvet-Touch-65-pics-156-86-MB",
		"https://vipergirls.to/threads/4268984-01-02-2019-017-MPL-Amelia-Right-About-Now-73-pics-189-08-MB",
		"https://vipergirls.to/threads/7271262-2015-10-16-Amelia-Touch-Me-Now-(x60)",
		// "https://vipergirls.to/threads/11374567-Stacy-Cruz-Sunday-Funday-159-pictures-6048px-(4-Aug-2024)?p=195262111",
		// "https://vipergirls.to/threads/10639992-Lee-Anne-Classical-Beauty-138-pictures-px-(29-Apr-2024)?p=175797485",
		// "https://vipergirls.to/threads/9122962-MetArt-com-Lee-Anne-My-Pearls-(Sep-23-2023)",
		// "https://vipergirls.to/threads/9918011-MetArt-com-Lee-Anne-Pale-Pink-Lace-(Jan-29-2024)",
		// "https://vipergirls.to/threads/9291626-Lee-Anne-Pure-Glamour-x145-(10-27-23)",
		// "https://vipergirls.to/threads/9489852-Lee-Anne-Sultry-(x131)-2912x4368",
		// "https://vipergirls.to/threads/8789527-Lee-Anne-New-Style-x152-(07-31-23)",
		// "https://vipergirls.to/threads/9574122-Lee-Anne-Lee-Anne-85-pictures-5472px-(22-Dec-2023)",
		// "https://vipergirls.to/threads/7453834-Mary-Rock-That-Smile-x138-(10-22-22)",
		// "https://vipergirls.to/threads/10870156-Mary-Rock-Stunning-Beauty-x155-(05-27-24)",
		// "https://vipergirls.to/threads/4806096-Mary-Rock-Honey-I-m-Home-(14-10-2019)-218x",
		// "https://vipergirls.to/threads/4896475-Mary-Rock-Nancy-A-Pretty-Blonde-Pussy-Licking-Orgasms-(24-11-2019)-128x",
		// "https://vipergirls.to/threads/5034923-MetArt-com-Mary-Rock-Goldilocks-(Jan-31-2020)",
		// "https://vipergirls.to/threads/5439939-Watch4Beauty-com-Mary-Rock-Casting-Mary-Rock-(Jul-28-2020)",
		// "https://vipergirls.to/threads/5938521-Mary-Rock-in-Piqued-Interest?p=79857493#post79857493",
		// "https://vipergirls.to/threads/11234895-Mary-Rock-Magnificence-1-118-pictures-7008px-(19-Jul-2024)",
		// "https://vipergirls.to/threads/5276558-SexArt-com-Mary-Rock-Afternoon-Light-(May-12-2020)",
		// "https://vipergirls.to/threads/3633805-Mary-Rock-Rub-Down-x74-3600px-Apr-26-2018",
		// "https://vipergirls.to/threads/5980576-Mary-Rock-Dotted",
		// "https://vipergirls.to/threads/7269427-Cornelia-Mary-Rock-Lickers-In-White-68x-5000-x-3333px-January-25-2022",
		// "https://vipergirls.to/threads/5954104-Mary-Rock-Climax-(05-03-2021)-103x",
		// "https://vipergirls.to/threads/7231807-Mary-Rock-Personal-Passion-140-Photos-Jul-26-2022",
		// "https://vipergirls.to/threads/8176253-MetArt-com-Mary-Rock-Vivacious-(May-01-2023)",
		// "https://vipergirls.to/threads/9138388-2023-10-02-Mary-Rock-Enchanting-x145",
		// "https://vipergirls.to/threads/6407980-Amirah-Adara-Mary-Rock-Squats-and-Scissoring-75x-3000x2000-10-04-2021",
		// "https://vipergirls.to/threads/6171788-Mary-Rock-Prairie",
		// "https://vipergirls.to/threads/8720389-MetArt-com-Mary-Rock-Rising-Heat-(Jul-15-2023)",
		// "https://vipergirls.to/threads/9349969-MetArt-com-Mary-Rock-Flirty-Delight-(Nov-08-2023)",
		// "https://vipergirls.to/threads/7807060-MetArt-com-Mary-Rock-Enthralling-(Feb-09-2023)",
		// "https://vipergirls.to/threads/6845181-Mary-Rock-Lime-Love-x126-(March-6-2022)",
		// "https://vipergirls.to/threads/5370800-MetArt-com-Mary-Rock-Pastime-(Jun-26-2020)",
		// "https://vipergirls.to/threads/5453934-MetArt-com-Mary-Rock-Dazzle_1-Me-(Aug-04-2020)",
		// "https://vipergirls.to/threads/9258721-Mary-Rock-Challenge-(X121)-3648x5472",
		// "https://vipergirls.to/threads/9450664-Mary-Rock-Primitive-(X120)-4480x6720",
		// "https://vipergirls.to/threads/4982765-FemJoy-com-Mary-Rock-Seduction-(Jan-05-2020)",
		// "https://vipergirls.to/threads/6341698-Mary-Rock-in-Her-Passion-116-5500px-09-05-2021",
		// "https://vipergirls.to/threads/6224582-Mary-Rock-in-Glimmer-x118-5500px-07-16-2021",
		// "https://vipergirls.to/threads/6954056-Mary-Rock-Memorable-116-Photos-Apr-14-2022",
		// "https://vipergirls.to/threads/6280239-Mary-Rock-Solo-Travel-14-Aug",
		// "https://vipergirls.to/threads/5584658-Mary-Rock-TEASE-115-Photos-Oct-07-2020",
		// "https://vipergirls.to/threads/5724144-Mary-Rock-Roseate-x74-8688px-(12-10-2020)",
		// "https://vipergirls.to/threads/5854962-Mary-Rock-Come-Into-My-Bed-x74-5760px-01-25-2021",
		// "https://vipergirls.to/threads/5048643-Mary-Rock-in-New-Stockings-x120-5500px-(02-07-2020)?p=59361443&viewfull=1#post59361443",
		// "https://vipergirls.to/threads/10472317-Mary-Rock-Intimate-With-Nature-120-pictures-5040px-(11-Apr-2024)",
		// "https://vipergirls.to/threads/10130457-Mary-Rock-Glow-121-pictures-5472px-(26-Feb-2024)",
		// "https://vipergirls.to/threads/7035192-Mary-Rock-Installation-157-Photos-May-15-2022",
		// "https://vipergirls.to/threads/6446696-Mary-Rock-Soft-Leather-120-Photos-Oct-19-2021?p=92530142#post92530142",
		// "https://vipergirls.to/threads/9228655-Mary-Rock-Resistance-(X120)-3360x5040",
		// "https://vipergirls.to/threads/6243575-Mary-Rock-Feather-120-Photos-Jul-25-2021",
		// "https://vipergirls.to/threads/6203234-Mary-Rock-Urban-Pleasure-126-Photos-Jul-05-2021",
		// "https://vipergirls.to/threads/6151126-Mary-Rock-Zuzu-Sweet-Loving-Smile-(10-06-2021)-135x",
		// "https://vipergirls.to/threads/5861200-Mary-Rock-Beachrock-120-Photos-Jan-27-2021",
		// "https://vipergirls.to/threads/6282477-Mary-Rock-in-Shine-126-Photos-5000px",
		// "https://vipergirls.to/threads/5583460-SexArt-com-Mary-Rock-Lush-(Oct-06-2020)",
		// "https://vipergirls.to/threads/5393466-Mary-Rock-Warm-Light-(Jul-07-2020)-130x?p=66829071&viewfull=1#post66829071",
		// "https://vipergirls.to/threads/5036993-SexArt-com-Mary-Rock-Garter-Belt-(Feb-01-2020)",
		// "https://vipergirls.to/threads/4990561-Mary-Rock-Mary-Rock-120-pictures-6720px-(9-Jan-2020)?p=57936777&viewfull=1#post57936777",
		// "https://vipergirls.to/threads/6021281-Mary-Rock-Rock-Steady-68-Photos-April-13-2021-Upcoming-Release",
		// "https://vipergirls.to/threads/7573454-Lily-Chey-(Guerlain-Lilii-Natalia-E-Lily-C-Violetta-Raisa-Marcella-Anastasia)[range]",
		// "https://vipergirls.to/threads/7526563-Femjoy-(complete-collection-in-chronological-order)[range]",
		// "https://vipergirls.to/threads/6437219-Photodromm-Collection[range]",
		// "https://vipergirls.to/threads/4977729-Hegre-Archives[range]",
		// "https://vipergirls.to/threads/5478451-Amour-Angels-Heaven-of-Sensuality-complete-amp-updated[range]",
		// "https://vipergirls.to/threads/6122206-ATKingdom-All-Collections[range]",
		// "https://vipergirls.to/threads/7526563-Femjoy-(complete-collection-in-chronological-order)[range]",
		// "https://vipergirls.to/threads/5144955-***ISTRIPPER-MODEL-COLLECTIONS***[range]",
		// "https://vipergirls.to/threads/10158645-AJ-APPLEGATE-(aka-Kaylee-Evans-Ajay-Applegate-Danielle)",
		// "https://vipergirls.to/threads/10573735-RED-FOX-(aka-Michelle-H-Foxy-T-Marga-E-Micca-Michelle-Starr-Nalla-Naomi-Noemi-Ruda-Sereti-Zania-Burlechenko)[range]",
		// "https://vipergirls.to/threads/7049792-Nancy-Ace?highlight=nancy+a[range]",
	}

	var wg sync.WaitGroup

	for _, targetUrl := range targetUrls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			DownloadLink(url)
		}(targetUrl)
	}
	wg.Wait()
}

func DownloadLink(targetUrl string) {
	if !strings.Contains(targetUrl, "[range]") {
		DownloadGallery(targetUrl, "")
	} else {
		//get all posts in gallery
		posts := ListAllPosts(targetUrl)
		for _, post := range posts {
			postId := ""
			if strings.Contains(post, "#post") {
				split := strings.Split(post, "#post")
				postId = split[len(split)-1]
			}
			DownloadGallery(post, postId)
		}
	}
}

func ListAllPosts(targetUrl string) []string {
	targetUrl = strings.ReplaceAll(targetUrl, "[range]", "")

	if strings.Contains(targetUrl, "#post") {
		return []string{
			targetUrl,
		}
	}

	if strings.Contains(targetUrl, "/page") {
		targetUrl = strings.Split(targetUrl, "/page")[0]
	}

	targetUrl += "/page"
	postUrls := map[string]string{}
	shouldRun := true
	page := 1
	currentPage := fmt.Sprintf("%s%v", targetUrl, page)

	c := colly.NewCollector()

	c.OnHTML("[id^='post_message_']", func(e *colly.HTMLElement) {
		postId := strings.ReplaceAll(e.Attr("id"), "post_message_", "")
		_, ok := postUrls[postId]
		if ok {
			shouldRun = false
			return
		}

		postUrls[postId] = currentPage + "#post" + postId
	})

	for shouldRun {
		currentPage = fmt.Sprintf("%s%v", targetUrl, page)
		page = page + 1

		err := c.Visit(currentPage)
		if err != nil {
			panic(err)
		}
	}

	var urls []string
	for _, value := range postUrls {
		urls = append(urls, value)
	}

	return urls
}

func DownloadGallery(targetUrl string, title string) {
	c := colly.NewCollector()

	tempTarget := targetUrl
	if strings.Contains(tempTarget, "/page") {
		tempTarget = strings.Split(targetUrl, "/page")[0]
	}

	u, _ := url.Parse(tempTarget)
	u.RawQuery = ""

	postId := "post_message_"
	if strings.Contains(targetUrl, "#post") {
		split := strings.Split(targetUrl, "#post")
		postId += split[len(split)-1]
	}

	directory := "C:\\Users\\carlj\\Downloads\\" + sanitizeFolderName(path.Base(u.Path))

	if title != "" {
		directory += "-" + title
	}

	_ = os.Mkdir(directory, os.ModePerm)
	isFirstMatch := true
	// Find and visit all links
	c.OnHTML(fmt.Sprintf("div[id^='%s']", postId), func(e *colly.HTMLElement) {
		count := len(e.DOM.Find("a img").Nodes)
		if _, err := os.Stat(directory); err == nil {
			saved, _ := os.ReadDir(directory)
			if len(saved) >= count {
				fmt.Println("Already downloaded: " + targetUrl)
				return
			}
		}

		if isFirstMatch {
			e.ForEach("a img", func(i int, element *colly.HTMLElement) {
				if element.Attr("alt") != "View Post" {
					a := element.DOM.Parent()
					src, _ := a.Attr("href")
					switch {
					case strings.Contains(src, "imagebam"):
						src = RipImageBam(src)
					case strings.Contains(src, "imgbox"):
						src = RipImageBox(src)
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
					case strings.Contains(src, "pixxxels.cc") || strings.Contains(src, "freeimage.us"):
						src = ""
					case strings.Contains(src, "postimages.org"):
						src = RipPostImages(src)
					default:
						fmt.Printf("Error: Unknown image source %v on source %v", src, targetUrl)
					}

					if src != "" {
						filename := path.Base(src)

						err := DownloadFile(src, fmt.Sprintf("%s\\%s", directory, filename))
						if err != nil {
							fmt.Printf("Error: error while downloading (%v) from: %v\n%v\n", src, targetUrl, err)
						}
					}
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
	c := colly.NewCollector()

	c.OnHTML("#img", func(e *colly.HTMLElement) {
		src = e.Attr("src")
	})

	err := c.Visit(src)
	if err != nil {
		panic(err)
	}
	return src
}

func RipPostImages(src string) string {
	return src
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

func sanitizeFolderName(name string) string {
	// Regular expression to match characters not allowed in folder names
	invalidCharsRegex := regexp.MustCompile("[<>:\"/\\|?*]")

	// Replace invalid characters with underscores
	sanitizedName := invalidCharsRegex.ReplaceAllString(name, "_")

	// Ensure the name doesn't start or end with an underscore
	sanitizedName = strings.Trim(sanitizedName, "_")

	return sanitizedName
}
