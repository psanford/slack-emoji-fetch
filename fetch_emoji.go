package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

var apiToken = flag.String("api-token", "", "API token xox*")
var cookie = flag.String("cookie", "", "Cookie (only for session tokens)")
var fetchFiles = flag.Bool("fetch-images", false, "Fetch images (default is just to fetch metadata)")
var userAgent = flag.String("user-agent", "", "User agent")

func fetchAllEmojis(client *http.Client, token string) (map[string]EmojiDetail, error) {
	allEmojis := make(map[string]EmojiDetail)
	page := 1
	limit := 1000 // max allowed by the API

	for {
		params := url.Values{}
		params.Set("token", token)
		params.Set("limit", fmt.Sprintf("%d", limit))
		params.Set("page", fmt.Sprintf("%d", page))

		url := "https://slack.com/api/emoji.adminList"
		req, err := http.NewRequest("POST", url, bytes.NewReader([]byte(params.Encode())))
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		if err != nil {
			return nil, fmt.Errorf("create request: %v", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("do request: %v", err)
		}
		defer resp.Body.Close()

		var emojiResp AdminEmojiResponse
		if err := json.NewDecoder(resp.Body).Decode(&emojiResp); err != nil {
			return nil, fmt.Errorf("decode response: %v", err)
		}

		if !emojiResp.Ok {
			return nil, fmt.Errorf("slack API error: %s", emojiResp.Error)
		}

		for _, emoji := range emojiResp.Emoji {
			allEmojis[emoji.Name] = emoji
		}

		if page >= int(emojiResp.Paging.Pages) {
			break
		}
		page++

		time.Sleep(100 * time.Millisecond)
	}

	return allEmojis, nil
}

func main() {
	flag.Parse()

	if *apiToken == "" {
		log.Fatal("-api_token required")
	}

	client := &http.Client{}
	if *cookie != "" {
		jar, err := cookiejar.New(nil)
		if err != nil {
			log.Fatalf("create cookie jar: %v", err)
		}
		u, err := url.Parse("https://slack.com")
		if err != nil {
			log.Fatalf("parse url: %v", err)
		}

		fakeReq := fmt.Sprintf("GET / HTTP/1.0\r\nCookie: %s\r\n\r\n", *cookie)
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(fakeReq)))
		if err != nil {
			log.Fatalf("read request: %v", err)
		}

		jar.SetCookies(u, req.Cookies())
		client.Jar = jar
	}

	if *userAgent != "" {
		transport := &userAgentTransport{
			userAgent: *userAgent,
			transport: http.DefaultTransport,
		}
		client.Transport = transport
	}

	emojis, err := fetchAllEmojis(client, *apiToken)
	if err != nil {
		log.Fatalf("fetch emojis: %v", err)
	}

	w := csv.NewWriter(os.Stdout)
	header := []string{"name", "url", "date_created", "uploaded_by", "user_display_name", "user_id"}
	if err := w.Write(header); err != nil {
		log.Fatalf("write header: %v", err)
	}

	for name, emoji := range emojis {
		row := []string{
			name,
			emoji.URL,
			fmt.Sprintf("%d", emoji.Created),
			emoji.UserID,
			emoji.UserDisplayName,
			emoji.UserID,
		}
		if err := w.Write(row); err != nil {
			log.Fatalf("write row: %v", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatalf("csv flush: %v", err)
	}

	if *fetchFiles {
		dir, err := os.MkdirTemp("", "fetch-emoji")
		if err != nil {
			log.Fatalf("make temp dir err: %s", err)
		}
		log.Printf("Downloading images to: %s", dir)

		ctx := context.Background()
		limiter := rate.NewLimiter(rate.Every(110*time.Millisecond), 5)

		for name, emoji := range emojis {
			if strings.HasPrefix(emoji.URL, "https://") {
				limiter.Wait(ctx)

				resp, err := client.Get(emoji.URL)
				if err != nil {
					log.Printf("fetch emoji %s %s err: %s", name, emoji.URL, err)
					continue
				}

				if resp.StatusCode != 200 {
					log.Printf("fetch emoji non-200 status %s %s status: %d", name, emoji.URL, resp.StatusCode)
					resp.Body.Close()
					continue
				}

				ext := path.Ext(emoji.URL)
				p := filepath.Join(dir, name+ext)
				outFile, err := os.Create(p)
				if err != nil {
					log.Printf("Create %q err %s", p, err)
					resp.Body.Close()
					continue
				}

				_, err = io.Copy(outFile, resp.Body)
				if err != nil {
					log.Printf("Save %s err: %s", emoji.URL, err)
				} else {
					log.Printf("Saved %s", p)
				}
				outFile.Close()
				resp.Body.Close()
			}
		}
	}
}

type userAgentTransport struct {
	userAgent string
	transport http.RoundTripper
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("User-Agent", t.userAgent)

	return t.transport.RoundTrip(req)
}

type AdminEmojiResponse struct {
	Ok                    bool          `json:"ok"`
	CustomEmojiTotalCount int64         `json:"custom_emoji_total_count"`
	Emoji                 []EmojiDetail `json:"emoji"`
	Paging                struct {
		Count int64 `json:"count"`
		Page  int64 `json:"page"`
		Pages int64 `json:"pages"`
		Total int64 `json:"total"`
	} `json:"paging"`
	Error string `json:"error,omitempty"`
}

type EmojiDetail struct {
	AliasFor        string   `json:"alias_for"`
	AvatarHash      string   `json:"avatar_hash"`
	CanDelete       bool     `json:"can_delete"`
	Created         int64    `json:"created"`
	IsAlias         int64    `json:"is_alias"`
	IsBad           bool     `json:"is_bad"`
	Name            string   `json:"name"`
	Synonyms        []string `json:"synonyms"`
	TeamID          string   `json:"team_id"`
	URL             string   `json:"url"`
	UserDisplayName string   `json:"user_display_name"`
	UserID          string   `json:"user_id"`
}
