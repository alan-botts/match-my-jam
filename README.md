# Match My Jam

Find the music you share with your friends.

Match My Jam pulls your full library from Spotify (playlists, liked songs, saved albums), stores it locally, and — once you and a friend are both synced — shows you the overlap.

## Status

Early. v1 ships Spotify login + sync. Gmail login + friend requests + overlap view are next. Apple Music and YouTube Music are on the roadmap.

## Stack

- Go 1.25, `chi` router
- SQLite (`modernc.org/sqlite` — pure Go, no CGO)
- `golang.org/x/oauth2` for Spotify + Google
- Signed-cookie sessions (`gorilla/securecookie`)
- Embedded HTML templates and static assets — one binary, no runtime deps
- Deploys on [Railway](https://railway.app)

## Running locally

```bash
export MMJ_BASE_URL=http://localhost:8080
export MMJ_SPOTIFY_CLIENT_ID=...
export MMJ_SPOTIFY_CLIENT_SECRET=...
export MMJ_SESSION_HASH_KEY=$(openssl rand -hex 32)
export MMJ_SESSION_BLOCK_KEY=$(openssl rand -hex 16)
export MMJ_DB_PATH=./data/mmj.db

go run ./cmd/match-my-jam
```

Open http://localhost:8080.

## Environment variables

| Name | Required | Description |
|------|----------|-------------|
| `MMJ_BASE_URL` | yes | Public base URL, used as OAuth redirect origin |
| `MMJ_SPOTIFY_CLIENT_ID` | yes | Spotify Developer app client id |
| `MMJ_SPOTIFY_CLIENT_SECRET` | yes | Spotify Developer app client secret |
| `MMJ_GOOGLE_CLIENT_ID` | no (v2) | Google OAuth client id |
| `MMJ_GOOGLE_CLIENT_SECRET` | no (v2) | Google OAuth client secret |
| `MMJ_SESSION_HASH_KEY` | yes | 64-hex-char HMAC key for session cookies |
| `MMJ_SESSION_BLOCK_KEY` | yes | 32-hex-char AES key for session cookies |
| `MMJ_DB_PATH` | no | SQLite path (default `./data/mmj.db`) |
| `PORT` | no | HTTP port (default `8080`) |

## License

MIT. See [LICENSE](LICENSE).
