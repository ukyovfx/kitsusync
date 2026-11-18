package setup

import (
	"bytes"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testGuildMemberListBody = `[{"user":{"id":"123456789012345680","username":"artist","global_name":"Artist Nick"},"nick":null,"roles":[],"joined_at":"2024-01-01T00:00:00Z"}]`

const testGuildRoleListBody = `[{"id":"123456789012345680","name":"Reviewers","managed":false,"mentionable":true}]`

// stubGuildDirectoryCache swaps the package cache for one whose fetch and
// clock the test controls, and exposes the fetch call count and the fake clock.
func stubGuildDirectoryCache(t *testing.T, memberCache bool) (*guildDirectoryCache, *atomic.Int32, *time.Time) {
	t.Helper()
	var old *guildDirectoryCache
	if memberCache {
		old = guildMemberListCache
		t.Cleanup(func() { guildMemberListCache = old })
	} else {
		old = guildRoleListCache
		t.Cleanup(func() { guildRoleListCache = old })
	}
	var calls atomic.Int32
	now := time.Unix(1700000000, 0)
	cache := &guildDirectoryCache{
		ttl:     guildDirectoryCacheTTL,
		entries: map[string]*guildDirectoryCacheEntry{},
		loads:   map[string]*guildDirectoryLoad{},
		fetch: func(guildID, botToken string) ([]byte, int, error) {
			calls.Add(1)
			return []byte(testGuildMemberListBody), http.StatusOK, nil
		},
		now: func() time.Time { return now },
	}
	if memberCache {
		guildMemberListCache = cache
	} else {
		cache.fetch = func(guildID, botToken string) ([]byte, int, error) {
			calls.Add(1)
			return []byte(testGuildRoleListBody), http.StatusOK, nil
		}
		guildRoleListCache = cache
	}
	return cache, &calls, &now
}

func TestGuildDirectoryCacheFreshViewDoesNotRefetch(t *testing.T) {
	for _, memberCache := range []bool{true, false} {
		t.Run(fmt.Sprintf("members=%v", memberCache), func(t *testing.T) {
			cache, calls, _ := stubGuildDirectoryCache(t, memberCache)
			if _, status, err := cache.refresh("g1", "tok"); err != nil || status != http.StatusOK {
				t.Fatalf("initial refresh failed: status=%d err=%v", status, err)
			}
			view := cache.view("g1", "tok")
			if !view.hasData() || !view.fresh || view.fetchFailed {
				t.Fatalf("fresh entry not served from cache: %+v", view)
			}
			if got := calls.Load(); got != 1 {
				t.Fatalf("fresh view refetched: %d calls, want 1", got)
			}
		})
	}
}

func TestGuildDirectoryCacheExpiredEntryRefetchesInBackground(t *testing.T) {
	cache, calls, clock := stubGuildDirectoryCache(t, true)
	if _, _, err := cache.refresh("g1", "tok"); err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	*clock = clock.Add(guildDirectoryCacheTTL + time.Minute)
	view := cache.view("g1", "tok")
	// The expired entry is still returned immediately; the refetch happens in
	// the background, so the page render never blocks.
	if !view.hasData() || view.fresh {
		t.Fatalf("expired entry should still show cached data: %+v", view)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		cache.mu.Lock()
		entry := cache.entries["g1"]
		fresh := entry != nil && !entry.fetchedAt.Before(cache.now().Add(-cache.ttl))
		cache.mu.Unlock()
		if fresh {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expired entry was not refetched (fetch calls: %d)", calls.Load())
		}
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("refetch fetch count: %d, want 2", got)
	}
}

func TestGuildDirectoryCacheFailedFetchKeepsOldBody(t *testing.T) {
	cache, calls, clock := stubGuildDirectoryCache(t, false)
	if _, _, err := cache.refresh("g1", "tok"); err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	*clock = clock.Add(guildDirectoryCacheTTL + time.Minute)
	cache.fetch = func(guildID, botToken string) ([]byte, int, error) {
		calls.Add(1)
		// botDo returns exactly this shape once its 429 retries run out.
		return []byte(`{"message":"synthetic rate limit"}`), http.StatusTooManyRequests, fmt.Errorf("discord API rate limited after 3 retries")
	}
	if _, status, err := cache.refresh("g1", "tok"); err == nil || status != http.StatusTooManyRequests {
		t.Fatalf("failed refresh should report the botDo result: status=%d err=%v", status, err)
	}
	view := cache.view("g1", "tok")
	if !view.hasData() || !bytes.Equal(view.body, []byte(testGuildRoleListBody)) {
		t.Fatalf("old cached body was not kept after a failed fetch: %s", view.body)
	}
	if view.fresh || !view.fetchFailed || !view.rateLimited {
		t.Fatalf("failed fetch must mark the view stale and rate-limited: %+v", view)
	}
	if notice := guildDirectoryNotice("en", view); notice != guildDirectoryStaleNoticeText("en", true) {
		t.Fatalf("stale view did not report the rate-limit notice: %q", notice)
	}
}

func TestGuildDirectoryCacheConcurrentRefreshFetchesOnce(t *testing.T) {
	cache, calls, _ := stubGuildDirectoryCache(t, true)
	release := make(chan struct{})
	cache.fetch = func(guildID, botToken string) ([]byte, int, error) {
		calls.Add(1)
		<-release
		return []byte(testGuildMemberListBody), http.StatusOK, nil
	}
	const callers = 8
	var wg sync.WaitGroup
	failures := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, status, err := cache.refresh("g1", "tok")
			if err != nil || status != http.StatusOK || !bytes.Equal(body, []byte(testGuildMemberListBody)) {
				failures <- err
			}
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("first fetch did not start: %d calls", calls.Load())
	}
	// Give the other callers time to join the in-flight load before releasing.
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	select {
	case err := <-failures:
		t.Fatalf("a joined caller got a different result: %v", err)
	default:
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent refreshes fetched %d times, want 1", got)
	}
}

func TestGuildDirectoryCacheInvalidateForcesRefetch(t *testing.T) {
	cache, calls, _ := stubGuildDirectoryCache(t, true)
	if _, _, err := cache.refresh("g1", "tok"); err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	cache.invalidate("g1")
	view := cache.view("g1", "tok")
	if view.hasData() {
		t.Fatalf("invalidate did not drop the cached entry: %+v", view)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("entry was not refetched after invalidate: %d calls", got)
	}
	view = cache.view("g1", "tok")
	if !view.hasData() || !view.fresh {
		t.Fatalf("refetched entry missing after invalidate: %+v", view)
	}
}

// TestCachedGuildDirectoryExposesNoSnapshotWhileFetchIsInFlight pins the display
// contract: with nothing cached the display read reports "no snapshot" at once
// instead of waiting for the fetch, which keeps a rate-limited page responsive.
func TestCachedGuildDirectoryExposesNoSnapshotWhileFetchIsInFlight(t *testing.T) {
	// cachedGuildMembers reads the seam directly (so every test stub works), so
	// the non-blocking guarantee lives in view(); pin it there.
	for _, memberCache := range []bool{true, false} {
		t.Run(fmt.Sprintf("members=%v", memberCache), func(t *testing.T) {
			cache, _, _ := stubGuildDirectoryCache(t, memberCache)
			block := make(chan struct{})
			cache.fetch = func(string, string) ([]byte, int, error) {
				<-block
				return nil, http.StatusTooManyRequests, fmt.Errorf("synthetic rate limit")
			}
			t.Cleanup(func() { close(block) })
			start := time.Now()
			view := cache.view("g1", "tok")
			if view.hasData() || view.fresh {
				t.Fatalf("empty cache should report no snapshot: %+v", view)
			}
			if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
				t.Fatalf("view waited on the fetch: %s", elapsed)
			}
		})
	}
}
