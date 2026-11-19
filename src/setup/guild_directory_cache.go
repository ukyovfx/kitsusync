package setup

import (
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Admin pages read the guild member and role lists through this in-process
// cache so a Discord rate limit degrades the display instead of blocking the
// page for minutes (botDo can wait 15 seconds three times on one 429 chain).
// Writes and saves call refresh, which keeps botDo's retry behaviour
// unchanged.

const guildDirectoryCacheTTL = 5 * time.Minute

// guildDirectoryCacheEntry keeps the raw response body so every decoder of
// that endpoint can reuse it.
type guildDirectoryCacheEntry struct {
	body        []byte
	fetchedAt   time.Time
	lastErr     error
	rateLimited bool
}

// guildDirectoryLoad shares one in-flight fetch between concurrent callers.
type guildDirectoryLoad struct {
	done   chan struct{}
	body   []byte
	status int
	err    error
}

type guildDirectoryCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]*guildDirectoryCacheEntry
	loads   map[string]*guildDirectoryLoad
	fetch   func(guildID, botToken string) ([]byte, int, error)
	now     func() time.Time
	// inflight counts the background refreshes that may call fetch, so a test
	// can wait for them before it restores fetch or swaps a shared seam.
	inflight sync.WaitGroup
}

var guildMemberListCache = &guildDirectoryCache{
	ttl:     guildDirectoryCacheTTL,
	entries: map[string]*guildDirectoryCacheEntry{},
	loads:   map[string]*guildDirectoryLoad{},
	fetch: func(guildID, botToken string) ([]byte, int, error) {
		return guildMemberListBody(guildID, botToken)
	},
	now: time.Now,
}

var guildRoleListCache = &guildDirectoryCache{
	ttl:     guildDirectoryCacheTTL,
	entries: map[string]*guildDirectoryCacheEntry{},
	loads:   map[string]*guildDirectoryLoad{},
	fetch: func(guildID, botToken string) ([]byte, int, error) {
		return guildRoleListBody(guildID, botToken)
	},
	now: time.Now,
}

// bindGuildDirectorySeams points the seam vars at the live readers. init keeps
// them out of the cache's initialization cycle.
func init() {
	reviewerGuildMembersForGuild = ListGuildMembers
	reviewerDiscordRolesForGuild = ListGuildRoles
}

// refresh fetches now, or joins a fetch already in flight. It reports what
// botDo would so the callers' existing error mapping works.
func (c *guildDirectoryCache) refresh(guildID, botToken string) ([]byte, int, error) {
	guildID = strings.TrimSpace(guildID)
	c.mu.Lock()
	if load := c.loads[guildID]; load != nil {
		c.mu.Unlock()
		<-load.done
		return load.body, load.status, load.err
	}
	load := &guildDirectoryLoad{done: make(chan struct{})}
	c.loads[guildID] = load
	c.mu.Unlock()
	body, status, err := c.fetch(guildID, botToken)
	load.body, load.status, load.err = body, status, err
	c.mu.Lock()
	delete(c.loads, guildID)
	entry := c.entries[guildID]
	if entry == nil {
		entry = &guildDirectoryCacheEntry{}
		c.entries[guildID] = entry
	}
	if err == nil && status < http.StatusBadRequest {
		entry.body, entry.fetchedAt, entry.lastErr, entry.rateLimited = body, c.now(), nil, false
	} else {
		// Keep the previous body: a later render can still show it with a
		// stale notice.
		fetchErr := err
		if fetchErr == nil {
			fetchErr = discordBotAPIError("discord guild directory refresh failed", status, body)
		}
		entry.lastErr, entry.rateLimited = fetchErr, status == http.StatusTooManyRequests
	}
	c.mu.Unlock()
	close(load.done)
	return body, status, err
}

type guildDirectoryView struct {
	body        []byte
	fresh       bool
	fetchFailed bool
	rateLimited bool
}

func (v guildDirectoryView) hasData() bool { return len(v.body) > 0 }

// view returns the cached snapshot without waiting on the network. A missing
// or expired entry only kicks a background refresh, so the page renders
// immediately and a later render picks up the new data.
func (c *guildDirectoryCache) view(guildID, botToken string) guildDirectoryView {
	guildID = strings.TrimSpace(guildID)
	c.mu.Lock()
	var v guildDirectoryView
	entry := c.entries[guildID]
	if entry != nil {
		v.body = entry.body
		v.fresh = !entry.fetchedAt.Before(c.now().Add(-c.ttl))
		v.fetchFailed = entry.lastErr != nil
		v.rateLimited = entry.rateLimited
	}
	if entry == nil || !v.fresh {
		if c.loads[guildID] == nil {
			c.inflight.Add(1)
			go func() {
				defer c.inflight.Done()
				_, _, _ = c.refresh(guildID, botToken)
			}()
		}
	}
	c.mu.Unlock()
	return v
}

// bypass reports whether tests replaced the reader this cache stands in for.
// Tests assign the seam functions directly, so comparing them to the
// production readers keeps every existing stub working without extra setup.
func (c *guildDirectoryCache) bypass() bool {
	if c == guildMemberListCache {
		return reviewerGuildMembersForGuild != nil && reflect.ValueOf(reviewerGuildMembersForGuild).Pointer() != reflect.ValueOf(ListGuildMembers).Pointer()
	}
	return reviewerDiscordRolesForGuild != nil && reflect.ValueOf(reviewerDiscordRolesForGuild).Pointer() != reflect.ValueOf(ListGuildRoles).Pointer()
}

// invalidate drops the cached lists so the next read fetches fresh data.
func (c *guildDirectoryCache) invalidate(guildID string) {
	guildID = strings.TrimSpace(guildID)
	if guildID == "" {
		return
	}
	c.mu.Lock()
	delete(c.entries, guildID)
	c.mu.Unlock()
}

// invalidateGuildDirectory drops both the member and role snapshots at once.
func invalidateGuildDirectory(guildID string) {
	guildMemberListCache.invalidate(guildID)
	guildRoleListCache.invalidate(guildID)
}

// memberListStatus describes what a cached member/role read can show.
type guildDirectoryStatus int

const (
	// guildDirectoryReady means a snapshot is available (possibly stale).
	guildDirectoryReady guildDirectoryStatus = iota
	// guildDirectoryPending means no snapshot exists yet and one is loading.
	guildDirectoryPending
)

// cachedGuildMembers reads the member list for display. It never waits on the
// network: a missing snapshot reports pending, a stale one reports ready so the
// page can add a freshness notice instead of dropping the data.
func cachedGuildMembers(guildID, botToken string) ([]DiscordGuildMember, guildDirectoryStatus, error) {
	if guildMemberListCache.bypass() {
		// A test replaced the reader, so use its result directly.
		members, err := reviewerGuildMembersForGuild(guildID, botToken)
		if err != nil {
			// With no snapshot to show, a failed read is the same as pending.
			return nil, guildDirectoryPending, nil
		}
		return members, guildDirectoryReady, nil
	}
	view := guildMemberListCache.view(guildID, botToken)
	if !view.hasData() {
		return nil, guildDirectoryPending, nil
	}
	members, err := decodeGuildMemberListBody(guildID, view.body, http.StatusOK)
	if err != nil {
		return nil, guildDirectoryReady, err
	}
	return members, guildDirectoryReady, nil
}

// cachedGuildRoles is the role-list counterpart of cachedGuildMembers.
func cachedGuildRoles(guildID, botToken string) ([]DiscordGuildRole, guildDirectoryStatus, error) {
	if guildRoleListCache.bypass() {
		roles, err := reviewerDiscordRolesForGuild(guildID, botToken)
		return roles, guildDirectoryReady, err
	}
	view := guildRoleListCache.view(guildID, botToken)
	if !view.hasData() {
		return nil, guildDirectoryPending, nil
	}
	roles, err := decodeGuildRoleListBody(view.body)
	if err != nil {
		return nil, guildDirectoryReady, err
	}
	return roles, guildDirectoryReady, nil
}

// guildDirectoryDegraded reports whether the Discord member or role snapshot is
// missing or could not be refreshed, so a page never shows a healthy state
// while the data behind it is unavailable.
func guildDirectoryDegraded(guildID, botToken string) bool {
	if botToken == "" || !isDiscordSnowflake(guildID) {
		return false
	}
	views := []guildDirectoryView{
		guildMemberListCache.view(guildID, botToken),
		guildRoleListCache.view(guildID, botToken),
	}
	for _, view := range views {
		if !view.hasData() || view.fetchFailed {
			return true
		}
	}
	return false
}

// guildDirectoryNotice returns the stale/pending notice for a cached read, or
// "" when the snapshot is fresh enough that nothing needs saying.
func guildDirectoryNotice(lang string, view guildDirectoryView) string {
	if !view.fetchFailed {
		return ""
	}
	if !view.hasData() {
		return ""
	}
	return guildDirectoryStaleNoticeText(lang, view.rateLimited)
}

// guildNoticeMarkup renders a freshness notice, or nothing when it is empty.
func guildNoticeMarkup(notice string) string {
	if notice == "" {
		return ""
	}
	return `<p class="field-help guild-directory-notice" role="status">` + esc(notice) + `</p>`
}

// guildDirectoryStaleNoticeText words the cached-stale notice once for every
// page that shows a stale member or role list.
func guildDirectoryStaleNoticeText(lang string, rateLimited bool) string {
	if rateLimited {
		return t(lang, "Discordの制限中のため、少し古いメンバー・ロール情報を表示しています。", "Discord is rate limiting the requests, so the member and role information shown is slightly old.")
	}
	return t(lang, "メンバー・ロール情報を再取得できなかったため、少し古い情報を表示しています。", "The member and role information could not be refreshed, so slightly old data is shown.")
}
