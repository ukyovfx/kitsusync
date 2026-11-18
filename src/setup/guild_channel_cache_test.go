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

const testChannelListBody = `[{"id":"f-anim","name":"animation","type":15,"available_tags":[{"id":"t1","name":"WFA"}]}]`

// stubGuildChannelCache swaps the package cache for one whose fetch and clock
// the test controls, and exposes the fetch call count and the fake clock.
func stubGuildChannelCache(t *testing.T) (*guildChannelsCache, *atomic.Int32, *time.Time) {
	t.Helper()
	old := guildChannelListCache
	t.Cleanup(func() { guildChannelListCache = old })
	var calls atomic.Int32
	now := time.Unix(1700000000, 0)
	cache := &guildChannelsCache{
		ttl:     guildChannelCacheTTL,
		entries: map[string]*guildChannelCacheEntry{},
		loads:   map[string]*guildChannelLoad{},
		fetch: func(guildID, botToken string) ([]byte, int, error) {
			calls.Add(1)
			return []byte(testChannelListBody), http.StatusOK, nil
		},
		now: func() time.Time { return now },
	}
	guildChannelListCache = cache
	return cache, &calls, &now
}

func TestGuildChannelCacheFreshViewDoesNotRefetch(t *testing.T) {
	cache, calls, _ := stubGuildChannelCache(t)
	if _, status, err := cache.refresh("g1", "tok"); err != nil || status != http.StatusOK {
		t.Fatalf("initial refresh failed: status=%d err=%v", status, err)
	}
	view := cache.view("g1", "tok")
	if !view.hasData() || !view.fresh || view.fetchFailed {
		t.Fatalf("fresh entry not served from cache: %+v", view)
	}
	if !bytes.Equal(view.body, []byte(testChannelListBody)) {
		t.Fatalf("view body changed: %s", view.body)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fresh view refetched: %d calls, want 1", got)
	}
}

func TestGuildChannelCacheExpiredEntryRefetchesInBackground(t *testing.T) {
	cache, calls, clock := stubGuildChannelCache(t)
	if _, _, err := cache.refresh("g1", "tok"); err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	*clock = clock.Add(guildChannelCacheTTL + time.Minute)
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

func TestGuildChannelCacheFailedFetchKeepsOldBody(t *testing.T) {
	cache, calls, clock := stubGuildChannelCache(t)
	if _, _, err := cache.refresh("g1", "tok"); err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	*clock = clock.Add(guildChannelCacheTTL + time.Minute)
	cache.fetch = func(guildID, botToken string) ([]byte, int, error) {
		calls.Add(1)
		// botDo returns exactly this shape once its 429 retries run out.
		return []byte(`{"message":"synthetic rate limit"}`), http.StatusTooManyRequests, fmt.Errorf("discord API rate limited after 3 retries")
	}
	if _, status, err := cache.refresh("g1", "tok"); err == nil || status != http.StatusTooManyRequests {
		t.Fatalf("failed refresh should report the botDo result: status=%d err=%v", status, err)
	}
	view := cache.view("g1", "tok")
	if !view.hasData() || !bytes.Equal(view.body, []byte(testChannelListBody)) {
		t.Fatalf("old cached body was not kept after a failed fetch: %s", view.body)
	}
	if view.fresh || !view.fetchFailed || !view.rateLimited {
		t.Fatalf("failed fetch must mark the view stale and rate-limited: %+v", view)
	}
}

func TestGuildChannelCacheConcurrentRefreshFetchesOnce(t *testing.T) {
	cache, calls, _ := stubGuildChannelCache(t)
	release := make(chan struct{})
	cache.fetch = func(guildID, botToken string) ([]byte, int, error) {
		calls.Add(1)
		<-release
		return []byte(testChannelListBody), http.StatusOK, nil
	}
	const callers = 8
	var wg sync.WaitGroup
	failures := make(chan error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, status, err := cache.refresh("g1", "tok")
			if err != nil || status != http.StatusOK || !bytes.Equal(body, []byte(testChannelListBody)) {
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

func TestGuildChannelCacheInvalidateForcesRefetch(t *testing.T) {
	cache, calls, _ := stubGuildChannelCache(t)
	if _, _, err := cache.refresh("g1", "tok"); err != nil {
		t.Fatalf("initial refresh failed: %v", err)
	}
	cache.invalidate("g1")
	view := cache.view("g1", "tok")
	if view.hasData() {
		t.Fatalf("invalidate did not drop the cached entry: %+v", view)
	}
	// The next view kicks a fresh fetch in the background (the app created a
	// channel, so the old list must never come back).
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
