package sync

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/alan-botts/match-my-jam/internal/store"
	"golang.org/x/oauth2"
)

const (
	// A sync_run that's been "running" for longer than this is considered stale.
	staleSyncThreshold = 5 * time.Minute
	// How often the watchdog checks for stale syncs.
	watchdogInterval = 2 * time.Minute
)

// Watchdog periodically checks for stale/aborted sync runs and re-kicks them.
// A sync is considered stale if its status is "running" but it started more
// than staleSyncThreshold ago (the goroutine was killed by a deploy, crash, etc).
type Watchdog struct {
	Store     *store.Store
	OAuthConf *oauth2.Config
}

func (w *Watchdog) Run(ctx context.Context) {
	// On startup, immediately check for stale syncs (handles deploy-killed goroutines).
	w.checkAndRecover(ctx)

	ticker := time.NewTicker(watchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.checkAndRecover(ctx)
		}
	}
}

func (w *Watchdog) checkAndRecover(ctx context.Context) {
	cutoff := time.Now().Add(-staleSyncThreshold)

	rows, err := w.Store.DB.QueryContext(ctx,
		`SELECT sr.id, sr.user_id, sr.provider, sr.started_at
		 FROM sync_runs sr
		 WHERE sr.status = 'running' AND sr.started_at < ?
		 ORDER BY sr.started_at ASC`, cutoff,
	)
	if err != nil {
		log.Printf("watchdog: query stale syncs: %v", err)
		return
	}
	defer rows.Close()

	type staleRun struct {
		ID        int64
		UserID    int64
		Provider  string
		StartedAt time.Time
	}
	var stale []staleRun
	for rows.Next() {
		var s staleRun
		if err := rows.Scan(&s.ID, &s.UserID, &s.Provider, &s.StartedAt); err != nil {
			log.Printf("watchdog: scan: %v", err)
			continue
		}
		stale = append(stale, s)
	}

	// Deduplicate by user+provider — only re-kick once per user.
	kicked := map[int64]bool{}
	for _, s := range stale {
		age := time.Since(s.StartedAt).Round(time.Second)
		log.Printf("watchdog: marking stale sync run=%d user=%d provider=%s age=%s as aborted", s.ID, s.UserID, s.Provider, age)

		// Mark the stale run as aborted.
		_, _ = w.Store.DB.ExecContext(ctx,
			`UPDATE sync_runs SET status = 'aborted', finished_at = CURRENT_TIMESTAMP, error = 'aborted by watchdog (stale)' WHERE id = ?`, s.ID,
		)

		if kicked[s.UserID] {
			continue
		}
		kicked[s.UserID] = true

		// Verify the user still exists and has an auth account for this provider.
		_, err := w.Store.GetAuthAccount(ctx, s.UserID, s.Provider)
		if err == sql.ErrNoRows {
			log.Printf("watchdog: user=%d has no %s account, skipping re-kick", s.UserID, s.Provider)
			continue
		}
		if err != nil {
			log.Printf("watchdog: get auth account user=%d: %v", s.UserID, err)
			continue
		}

		// Re-kick the sync in a new goroutine.
		syncer := &SpotifySyncer{Store: w.Store, OAuthConf: w.OAuthConf}
		go func(uid int64) {
			log.Printf("sync starting user=%d (watchdog re-kick)", uid)
			bg, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			if err := syncer.Run(bg, uid); err != nil {
				log.Printf("watchdog sync user=%d: %v", uid, err)
			} else {
				log.Printf("sync complete user=%d (watchdog re-kick)", uid)
			}
		}(s.UserID)
	}

	if len(stale) == 0 {
		log.Printf("watchdog: no stale syncs found")
	}
}
