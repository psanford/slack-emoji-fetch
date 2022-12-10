package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"golang.org/x/time/rate"
)

var apiToken = flag.String("api-token", "", "API token xox*")
var cookie = flag.String("cookie", "", "Cookie (only for session tokens)")
var fetchFiles = flag.Bool("fetch-images", false, "Fetch images (default is just to fetch metadata)")

func main() {
	flag.Parse()

	if *apiToken == "" {
		log.Fatal("-api_token required")
	}

	var options []slack.Option
	if *cookie != "" {
		jar, err := cookiejar.New(nil)
		if err != nil {
			panic(err)
		}
		u, err := url.Parse("https://slack.com")
		if err != nil {
			panic(err)
		}

		fakeReq := fmt.Sprintf("GET / HTTP/1.0\r\nCookie: %s\r\n\r\n", *cookie)
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(fakeReq)))
		if err != nil {
			panic(err)
		}

		jar.SetCookies(u, req.Cookies())
		client := http.Client{
			Jar: jar,
		}
		options = append(options, slack.OptionHTTPClient(&client))
	}

	api := slack.New(*apiToken, options...)

	emojis, err := api.GetEmoji()
	if err != nil {
		panic(err)
	}

	w := csv.NewWriter(os.Stdout)

	w.Write([]string{"name", "url"})

	for k, v := range emojis {
		w.Write([]string{k, v})
	}

	w.Flush()
	err = w.Error()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()

	limiter := rate.NewLimiter(rate.Every(110*time.Millisecond), 5)

	dir, err := ioutil.TempDir("", "fetch-emoji")
	if err != nil {
		log.Fatalf("make temp dir err: %s", err)
	}
	if *fetchFiles {
		for k, v := range emojis {
			if strings.HasPrefix(v, "https://") {
				resp, err := http.Get(v)
				if err != nil {
					log.Printf("fetch emoji %s %s err: %s", k, v, err)
					continue
				}

				if resp.StatusCode != 200 {
					log.Printf("fetch emoji non-200 status %s %s status: %d", k, v, resp.StatusCode)
					continue
				}

				ext := path.Ext(v)
				p := filepath.Join(dir, k+ext)
				outFile, err := os.Create(p)
				if err != nil {
					log.Printf("Create %q err %s", p, err)
					continue
				}

				_, err = io.Copy(outFile, resp.Body)
				if err != nil {
					log.Printf("Save %s err: %s", v, err)
				} else {
					log.Printf("Saved %s", p)
				}
				outFile.Close()

				limiter.Wait(ctx)
			}
		}
	}
}
