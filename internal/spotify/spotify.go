package spotify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

const Provider = "spotify"

var Endpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.spotify.com/authorize",
	TokenURL:  "https://accounts.spotify.com/api/token",
	AuthStyle: oauth2.AuthStyleInHeader,
}

var Scopes = []string{
	"user-read-email",
	"user-read-private",
	"user-library-read",
	"playlist-read-private",
	"playlist-read-collaborative",
}

func OAuthConfig(clientID, clientSecret, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       Scopes,
		Endpoint:     Endpoint,
	}
}

type Client struct {
	http *http.Client
}

func NewClient(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) *Client {
	return &Client{http: cfg.Client(ctx, tok)}
}

func NewClientFromHTTP(httpClient *http.Client) *Client {
	return &Client{http: httpClient}
}

type Profile struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Images      []struct {
		URL string `json:"url"`
	} `json:"images"`
}

func (c *Client) Me(ctx context.Context) (*Profile, error) {
	var p Profile
	if err := c.getJSON(ctx, "https://api.spotify.com/v1/me", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

type Paging struct {
	Next  string          `json:"next"`
	Items json.RawMessage `json:"items"`
	Total int             `json:"total"`
}

type PlaylistSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	SnapshotID    string `json:"snapshot_id"`
	Public        bool   `json:"public"`
	Collaborative bool   `json:"collaborative"`
	Owner         struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"owner"`
	Tracks struct {
		Total int `json:"total"`
	} `json:"tracks"`
}

func (c *Client) AllPlaylists(ctx context.Context) ([]PlaylistSummary, error) {
	var out []PlaylistSummary
	next := "https://api.spotify.com/v1/me/playlists?limit=50"
	for next != "" {
		var page Paging
		if err := c.getJSON(ctx, next, &page); err != nil {
			return nil, err
		}
		var items []PlaylistSummary
		if err := json.Unmarshal(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		next = page.Next
	}
	return out, nil
}

type PlaylistTrack struct {
	AddedAt string `json:"added_at"`
	Track   *Track `json:"track"`
}

type Track struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	DurationMs int      `json:"duration_ms"`
	Artists    []Artist `json:"artists"`
	Album      struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	} `json:"album"`
}

type Artist struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

func (t *Track) ArtistNames() string {
	if t == nil || len(t.Artists) == 0 {
		return ""
	}
	out := t.Artists[0].Name
	for _, a := range t.Artists[1:] {
		out += ", " + a.Name
	}
	return out
}

func (c *Client) PlaylistTracks(ctx context.Context, playlistID string) ([]PlaylistTrack, error) {
	var out []PlaylistTrack
	next := fmt.Sprintf("https://api.spotify.com/v1/playlists/%s/tracks?limit=100&fields=next,items(added_at,track(id,name,duration_ms,artists(id,name),album(id,name)))", url.PathEscape(playlistID))
	for next != "" {
		var page Paging
		if err := c.getJSON(ctx, next, &page); err != nil {
			return nil, err
		}
		var items []PlaylistTrack
		if err := json.Unmarshal(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		next = page.Next
	}
	return out, nil
}

type SavedTrack struct {
	AddedAt string `json:"added_at"`
	Track   Track  `json:"track"`
}

func (c *Client) LikedTracks(ctx context.Context) ([]SavedTrack, error) {
	var out []SavedTrack
	next := "https://api.spotify.com/v1/me/tracks?limit=50"
	for next != "" {
		var page Paging
		if err := c.getJSON(ctx, next, &page); err != nil {
			return nil, err
		}
		var items []SavedTrack
		if err := json.Unmarshal(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		next = page.Next
	}
	return out, nil
}

type SavedAlbum struct {
	AddedAt string `json:"added_at"`
	Album   struct {
		ID      string   `json:"id"`
		Name    string   `json:"name"`
		Artists []Artist `json:"artists"`
	} `json:"album"`
}

func (c *Client) SavedAlbums(ctx context.Context) ([]SavedAlbum, error) {
	var out []SavedAlbum
	next := "https://api.spotify.com/v1/me/albums?limit=50"
	for next != "" {
		var page Paging
		if err := c.getJSON(ctx, next, &page); err != nil {
			return nil, err
		}
		var items []SavedAlbum
		if err := json.Unmarshal(page.Items, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		next = page.Next
	}
	return out, nil
}

func (c *Client) getJSON(ctx context.Context, urlStr string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		retry := resp.Header.Get("Retry-After")
		if retry == "" {
			retry = "1"
		}
		var secs int
		fmt.Sscanf(retry, "%d", &secs)
		if secs < 1 {
			secs = 1
		}
		select {
		case <-time.After(time.Duration(secs) * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		return c.getJSON(ctx, urlStr, out)
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("spotify %s: %s: %s", urlStr, resp.Status, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
