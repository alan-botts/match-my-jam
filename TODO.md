# Match My Jam — TODO

Status snapshot as of 2026-04-05. Checked items are done; unchecked items are outstanding.

## Shipped

- [x] Go scaffold (`cmd/match-my-jam` + `internal/{config,db,store,session,spotify,googleauth,sync,web}`)
- [x] SQLite schema and auto-migrate: users, auth_accounts, playlists, playlist_tracks, liked_tracks, saved_albums, friendships, sync_runs
- [x] Spotify OAuth login + callback with CSRF state cookie
- [x] Spotify library sync worker: playlists + tracks, liked songs, saved albums, token-refresh persisted, 403/404 on individual playlists is logged-and-skipped
- [x] Google OAuth login wired end-to-end (awaiting Google Cloud OAuth client creation to flip it on)
- [x] Automatic account linking: same email → same user, multiple provider rows
- [x] Friends: search by email, send request, accept, decline, friends list
- [x] Overlap view: shared liked tracks, shared saved albums, tracks found in playlists of both users, all computed via SQL joins
- [x] Settings page: connected sign-ins, disconnect (refuses the last one), delete account with confirmation
- [x] Dark mode UI with pink/red/orange gradient buttons, nav, flash messages
- [x] Dockerfile + `railway.json`, deployed to Railway staging at <https://match-my-jam-staging.up.railway.app>
- [x] Persistent Railway volume mounted at `/data` for SQLite
- [x] Session cookies signed with `gorilla/securecookie`
- [x] Template namespace collision fix (each page is its own top-level template)

## Must-do next

- [ ] **End-to-end login test on staging** — Kyle authorizes the Spotify app, verifies dashboard shows live counts, resyncs, checks dashboard again
- [ ] **Request Spotify extended quota** so non-owned playlists (curated, followed soundtracks, etc.) stop returning 403
- [ ] **Google Cloud OAuth client** — create Google Cloud project, OAuth consent screen, web client id/secret, set `MMJ_GOOGLE_CLIENT_ID` and `MMJ_GOOGLE_CLIENT_SECRET` in Railway
- [ ] **Promote to production** — after staging is validated, create `production` environment in Railway and deploy
- [ ] **Custom domain** — decide whether to wire `matchmyjam.com` (or similar) to the Railway service

## Should-do soon

- [ ] **Incremental sync** — current sync is full-refresh (delete-then-insert for liked + albums). Switch to upsert by `added_at` so it's cheaper and tracks unsave events
- [ ] **Sync progress indicator** — the dashboard shows counts but no "currently syncing" spinner; dashboard polls would be nice
- [ ] **Profile display names on friend requests** — currently uses Spotify display name; Google sign-in would override; pick a sensible merge rule
- [ ] **Overlap sort/filter** — by artist, by date added, by shared-most-recent
- [ ] **Artist-level overlap** — right now we match on `provider_track_id` exactly, which misses the same song on different releases. Add an artist+track-name fallback
- [ ] **Rate-limit handling on 429** — `spotify.Client` retries on 429 with `Retry-After`, but we should also surface that in the sync_runs log
- [ ] **Pagination of overlap results** — currently caps at 2000 playlist hits; paginate UI for large libraries
- [ ] **Empty state for first-time users** — explain that sync takes ~1 min before the dashboard has data

## Nice-to-haves / roadmap

- [ ] **Apple Music** — MusicKit JS + Apple Developer token, MUS store-front, fetch user library
- [ ] **YouTube Music** — no official Go lib; either wrap `ytmusicapi` (Python) in a tiny sidecar or hand-roll the internal API calls
- [ ] **Weekly digest email** — "new overlaps with friends this week"
- [ ] **Share-card generator** — OG image endpoint that renders a PNG of a friend overlap, for posting
- [ ] **Listen-along / jam session mode** — queue the overlap playlist on the most recently active device
- [ ] **Friend activity feed** — when a friend likes a new song that you also like, surface it

## Known issues

- [ ] Spotify dev apps without extended quota return 403 on many non-owned playlists. Currently handled by skipping; ideally we request the quota extension.
- [ ] Session secret keys are generated per-deploy. Rotating them logs everyone out. Fine for staging; prod should persist them.
- [ ] `playlist_tracks.position` is an `INTEGER` unique per playlist, so reordering a playlist requires a full rewrite. That's by design (we do `DELETE + INSERT` inside a transaction), but worth noting.
- [ ] No CSRF protection on form POSTs beyond same-site cookies. Good enough for v1, add double-submit token before prod.

## DevOps

- [ ] Add `railway logs` + deploy notification hook to the goated cron watchdog
- [ ] Periodic DB backup (Railway's volume is not automatically backed up)
- [ ] Structured logging (`log/slog`) with request id on every line
- [ ] Basic metrics endpoint (request count, sync duration) at `/metrics`
