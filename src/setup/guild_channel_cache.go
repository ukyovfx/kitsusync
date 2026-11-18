package setup

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Admin pages read the guild channel list through this in-process cache so a
// Discord rate limit degrades the display instead of blocking the page for up
// to 45 seconds. Actions that need fresh data (saves, the manual refetch
// route) call refresh, keeping botDo's retry behaviour unchanged.

const guildChannelCacheTTL = 5 * time.Minute

// guildChannelCacheEntry keeps the raw response body so both decoders
// (channels and forum channels, which needs available_tags) can reuse it.
type guildChannelCacheEntry struct {
	body        []byte
	fetchedAt   time.Time
	lastErr     error
	rateLimited bool
}

// guildChannelLoad shares one in-flight fetch between concurrent callers.
type guildChannelLoad struct {
	done   chan struct{}
	body   []byte
	status int
	err    error
}

type guildChannelsCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*guildChannelCacheEntry
	loads   map[string]*guildChannelLoad
	fetch   func(guildID, botToken string) ([]byte, int, error)
	now     func() time.Time
}

// guildChannelListBody performs the same GET the list readers used to do
// inline.
func guildChannelListBody(guildID, botToken string) ([]byte, int, error) {
	return botDo(http.MethodGet, discordAPI+"/guilds/"+url.PathEscape(strings.TrimSpace(guildID))+"/channels", nil, botToken)
}

// guildChannelListCache backs both list readers. Tests replace the variable
// (and with it the fetch seam) so no request leaves the process.
var guildChannelListCache = &guildChannelsCache{
	ttl:     guildChannelCacheTTL,
	entries: map[string]*guildChannelCacheEntry{},
	loads:   map[string]*guildChannelLoad{},
	fetch:   guildChannelListBody,
	now:     time.Now,
}

// refresh fetches the channel list now, or joins a fetch already in flight.
// It reports what botDo would so the callers' existing error mapping works.
func (c *guildChannelsCache) refresh(guildID, botToken string) ([]byte, int, error) {
	guildID = strings.TrimSpace(guildID)
	c.mu.Lock()
	if load := c.loads[guildID]; load != nil {
		c.mu.Unlock()
		<-load.done
		return load.body, load.status, load.err
	}
	load := &guildChannelLoad{done: make(chan struct{})}
	c.loads[guildID] = load
	c.mu.Unlock()
	body, status, err := c.fetch(guildID, botToken)
	load.body, load.status, load.err = body, status, err
	c.mu.Lock()
	delete(c.loads, guildID)
	entry := c.entries[guildID]
	if entry == nil {
		entry = &guildChannelCacheEntry{}
		c.entries[guildID] = entry
	}
	if err == nil && status < http.StatusBadRequest {
		entry.body, entry.fetchedAt, entry.lastErr, entry.rateLimited = body, c.now(), nil, false
	} else {
		// Keep the previous body: a later render can still show it with a
		// stale notice.
		fetchErr := err
		if fetchErr == nil {
			fetchErr = discordBotAPIError("discord guild channel list refresh failed", status, body)
		}
		entry.lastErr, entry.rateLimited = fetchErr, status == http.StatusTooManyRequests
	}
	c.mu.Unlock()
	close(load.done)
	return body, status, err
}

type guildChannelView struct {
	body        []byte
	fresh       bool
	fetchFailed bool
	rateLimited bool
}

func (v guildChannelView) hasData() bool { return len(v.body) > 0 }

// view returns the cached snapshot without waiting on the network. A missing
// or expired entry only kicks a background refresh, so the page renders
// immediately and a later render picks up the new data.
func (c *guildChannelsCache) view(guildID, botToken string) guildChannelView {
	guildID = strings.TrimSpace(guildID)
	c.mu.Lock()
	var v guildChannelView
	entry := c.entries[guildID]
	if entry != nil {
		v.body = entry.body
		v.fresh = !entry.fetchedAt.Before(c.now().Add(-c.ttl))
		v.fetchFailed = entry.lastErr != nil
		v.rateLimited = entry.rateLimited
	}
	if entry == nil || !v.fresh {
		if c.loads[guildID] == nil {
			go c.refresh(guildID, botToken)
		}
	}
	c.mu.Unlock()
	return v
}

// invalidate drops the cached list after this app changed the guild's
// channels, so the next read fetches fresh data.
func (c *guildChannelsCache) invalidate(guildID string) {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, guildID)
	c.mu.Unlock()
}

// guildChannelStaleNoticeText words the cached-stale notice once for every
// page that shows a cached channel list past its TTL whose refresh failed.
func guildChannelStaleNoticeText(lang string, rateLimited bool) string {
	if rateLimited {
		return t(lang, "Discordの制限中のため、少し古いチャンネル情報を表示しています。", "Discord is rate limiting the requests, so the channel information shown is slightly old.")
	}
	return t(lang, "チャンネル一覧を再取得できなかったため、少し古いチャンネル情報を表示しています。", "The channel list could not be refreshed, so the channel information shown is slightly old.")
}

// RefreshDiscordChannelsHandler re-reads a guild's channel list ignoring the
// cache, then redirects back so the page shows the result. The fetch is
// synchronous on purpose: the operator explicitly asked to wait for Discord.
func RefreshDiscordChannelsHandler(discordSettings func() (botToken string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		back := r.FormValue("back")
		// Only same-app paths, so the redirect cannot leave this app.
		if !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.HasPrefix(back, "/\\") {
			back = "/bot/admin"
		}
		if guildID := strings.TrimSpace(r.FormValue("guild")); guildID != "" {
			_, _, _ = guildChannelListCache.refresh(guildID, discordSettings())
		}
		http.Redirect(w, r, withLang(back, r), http.StatusSeeOther)
	}
}
