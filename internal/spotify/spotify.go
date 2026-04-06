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
	Images []struct {
		URL string `json:"url"`
	} `json:"images"`
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

// PlaylistTrack represents a single entry in a playlist's item list.
// Spotify renamed the nested field from "track" to "item" in Feb 2026;
// we accept both via custom unmarshalling for backward compatibility.
type PlaylistTrack struct {
	AddedAt string `json:"added_at"`
	IsLocal bool   `json:"is_local"`
	Track   *Track `json:"-"` // populated from "item" or legacy "track"
}

// UnmarshalJSON handles both the new "item" field and the legacy "track" field.
func (pt *PlaylistTrack) UnmarshalJSON(data []byte) error {
	// Use an alias to avoid infinite recursion.
	type Alias struct {
		AddedAt string          `json:"added_at"`
		IsLocal bool            `json:"is_local"`
		Item    json.RawMessage `json:"item"`
		Track   json.RawMessage `json:"track"`
	}
	var a Alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	pt.AddedAt = a.AddedAt
	pt.IsLocal = a.IsLocal

	// Prefer "item" (new API), fall back to "track" (legacy).
	raw := a.Item
	if len(raw) == 0 || string(raw) == "null" {
		raw = a.Track
	}
	if len(raw) == 0 || string(raw) == "null" {
		pt.Track = nil
		return nil
	}
	var t Track
	if err := json.Unmarshal(raw, &t); err != nil {
		return err
	}
	pt.Track = &t
	return nil
}

type Track struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Type       string   `json:"type"` // "track" or "episode"
	IsLocal    bool     `json:"is_local"`
	DurationMs int      `json:"duration_ms"`
	Artists    []Artist `json:"artists"`
	Album      struct {
		Name   string `json:"name"`
		ID     string `json:"id"`
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
	} `json:"album"`
}

// IsEpisode reports whether this item is a podcast episode rather than a music track.
func (t *Track) IsEpisode() bool {
	return t != nil && t.Type == "episode"
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
	// Use the new /items endpoint (Feb 2026 API change) and request fields
	// that let us distinguish tracks, episodes, and local files.
	// We request both "item" (new) and "track" (legacy fallback) so parsing
	// works regardless of which field the API returns.
	next := fmt.Sprintf(
		"https://api.spotify.com/v1/playlists/%s/items?limit=100&fields=%s",
		url.PathEscape(playlistID),
		url.QueryEscape("next,items(added_at,is_local,item(id,name,type,is_local,duration_ms,artists(id,name),album(id,name,images)),track(id,name,type,is_local,duration_ms,artists(id,name),album(id,name,images)))"),
	)
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
		Images  []struct {
			URL string `json:"url"`
		} `json:"images"`
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
