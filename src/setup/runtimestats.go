package setup

import (
	"sync"
	"time"
)

const maxAPIObservations = 20

// APIObservation is a redacted, in-memory record of one external API health check.
// It deliberately contains no URL, credential, request, or response data.
type APIObservation struct {
	At             time.Time
	Duration       time.Duration
	Success        bool
	Classification string
}

// RuntimeStats tracks live operational metrics updated each polling cycle.
// Resets on app restart — acceptable for small-team self-hosted deployments.
type RuntimeStats struct {
	mu sync.RWMutex

	StartTime time.Time

	PollCount         int64
	LastPollTime      time.Time
	LastPollTaskCount int
	LastPollDuration  time.Duration
	LastPollErr       string

	SendSuccessTotal int64
	SendFailureTotal int64
	LastSendTime     time.Time

	webhookFailures map[string]int64
	webhookLastErr  map[string]string
	webhookLastSend map[string]time.Time
	apiObservations map[string][]APIObservation
	productionPolls map[string]*ProductionPollStat
}

// Stats is the singleton runtime stats instance used by main.go and the health handler.
var Stats = &RuntimeStats{
	StartTime:       time.Now(),
	webhookFailures: make(map[string]int64),
	webhookLastErr:  make(map[string]string),
	webhookLastSend: make(map[string]time.Time),
	apiObservations: make(map[string][]APIObservation),
}

// RecordAPIObservation adds one bounded, secret-safe observation for a service.
func (s *RuntimeStats) RecordAPIObservation(service string, started time.Time, success bool, classification string) {
	finished := time.Now()
	s.RecordAPIObservationAt(service, finished, finished.Sub(started), success, classification)
}

// RecordAPIObservationAt stores a probe duration at the shared monitoring-cycle timestamp.
func (s *RuntimeStats) RecordAPIObservationAt(service string, sampledAt time.Time, duration time.Duration, success bool, classification string) {
	s.RecordAPIObservationBatch(map[string]APIObservation{
		service: {At: sampledAt, Duration: duration, Success: success, Classification: classification},
	})
}

// RecordAPIObservationBatch appends one complete monitoring cycle atomically.
func (s *RuntimeStats) RecordAPIObservationBatch(observations map[string]APIObservation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.apiObservations == nil {
		s.apiObservations = make(map[string][]APIObservation)
	}
	for service, observation := range observations {
		if service == "" {
			continue
		}
		items := append(s.apiObservations[service], observation)
		if len(items) > maxAPIObservations {
			items = items[len(items)-maxAPIObservations:]
		}
		s.apiObservations[service] = items
	}
}

// ProductionPollStat is the per-production polling visibility snapshot.
// TaskCount is the raw number of tasks the bot observed for the production in
// the most recent successful poll, before any age filtering.
type ProductionPollStat struct {
	LastSuccessAt time.Time
	TaskCount     int
	ZeroStreak    int
	StallNotified bool
}

// RecordProductionPoll updates per-production polling visibility after one
// successful poll cycle. A zero count advances the consecutive-zero streak;
// a non-zero count clears both the streak and any prior stall notification.
func (s *RuntimeStats) RecordProductionPoll(productionID string, taskCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.productionPolls == nil {
		s.productionPolls = make(map[string]*ProductionPollStat)
	}
	stat, ok := s.productionPolls[productionID]
	if !ok {
		stat = &ProductionPollStat{}
		s.productionPolls[productionID] = stat
	}
	stat.LastSuccessAt = time.Now()
	stat.TaskCount = taskCount
	if taskCount > 0 {
		stat.ZeroStreak = 0
		stat.StallNotified = false
	} else {
		stat.ZeroStreak++
	}
}

// TakeStallNotification reports — exactly once per stall episode — whether a
// production has been open with three consecutive zero-task polls. The flag
// clears only when the production reports tasks again.
func (s *RuntimeStats) TakeStallNotification(productionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	stat, ok := s.productionPolls[productionID]
	if !ok {
		return false
	}
	if stat.ZeroStreak >= 3 && !stat.StallNotified {
		stat.StallNotified = true
		return true
	}
	return false
}

// ProductionPollStats returns a copy of the per-production polling snapshot.
func (s *RuntimeStats) ProductionPollStats() map[string]ProductionPollStat {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]ProductionPollStat, len(s.productionPolls))
	for id, stat := range s.productionPolls {
		out[id] = *stat
	}
	return out
}

// RecordPoll updates polling stats after one cycle completes successfully.
func (s *RuntimeStats) RecordPoll(taskCount int) {
	s.RecordPollWithDuration(taskCount, 0)
}

// RecordPollWithDuration records a successful poll and its measured duration.
func (s *RuntimeStats) RecordPollWithDuration(taskCount int, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PollCount++
	s.LastPollTime = time.Now()
	s.LastPollTaskCount = taskCount
	s.LastPollDuration = duration
	s.LastPollErr = ""
}

// RecordPollError records a failed poll cycle without updating the success counters.
func (s *RuntimeStats) RecordPollError(errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastPollErr = errMsg
}

// LastPollError returns the error message from the most recent failed poll cycle.
// Returns "" if the last poll succeeded or no poll has run yet.
func (s *RuntimeStats) LastPollError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastPollErr
}

// RecordSend updates Discord send stats for one webhook batch.
// A successful send to a webhook clears its failure counter.
func (s *RuntimeStats) RecordSend(sentCount, failedCount int, webhookURL, errSummary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.SendSuccessTotal += int64(sentCount)
	s.SendFailureTotal += int64(failedCount)
	if sentCount > 0 || failedCount > 0 {
		s.LastSendTime = time.Now()
	}
	if webhookURL == "" {
		return
	}
	if failedCount > 0 {
		s.webhookFailures[webhookURL] += int64(failedCount)
		if errSummary != "" {
			s.webhookLastErr[webhookURL] = errSummary
		}
	}
	if sentCount > 0 {
		delete(s.webhookFailures, webhookURL)
		delete(s.webhookLastErr, webhookURL)
		s.webhookLastSend[webhookURL] = time.Now()
	}
}

// WebhookInactive returns true if the webhook URL has not sent any message for
// at least threshold duration AND the app has been running at least that long.
func (s *RuntimeStats) WebhookInactive(webhookURL string, threshold time.Duration) bool {
	if time.Since(s.StartTime) < threshold {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	lastSend, ok := s.webhookLastSend[webhookURL]
	if !ok {
		return true
	}
	return time.Since(lastSend) > threshold
}

// WebhookHealthEntry describes failure state for one webhook URL since startup.
type WebhookHealthEntry struct {
	URL          string
	FailureCount int64
	LastError    string
}

// WebhookHealthList returns webhook URLs that have recorded failures since startup.
func (s *RuntimeStats) WebhookHealthList() []WebhookHealthEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var list []WebhookHealthEntry
	for url, count := range s.webhookFailures {
		if count > 0 {
			list = append(list, WebhookHealthEntry{
				URL:          url,
				FailureCount: count,
				LastError:    s.webhookLastErr[url],
			})
		}
	}
	return list
}

// WebhookLastSend returns the time of the last successful send for a given webhook URL.
// Returns zero time if no successful send has been recorded since startup.
func (s *RuntimeStats) WebhookLastSend(webhookURL string) time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.webhookLastSend[webhookURL]
}

// RuntimeSnapshot is a point-in-time copy of all runtime metrics.
type RuntimeSnapshot struct {
	StartTime         time.Time
	PollCount         int64
	LastPollTime      time.Time
	LastPollTaskCount int
	LastPollDuration  time.Duration
	LastPollErr       string
	SendSuccessTotal  int64
	SendFailureTotal  int64
	LastSendTime      time.Time
	APIObservations   map[string][]APIObservation
}

// Snapshot returns a safe copy of current metrics.
func (s *RuntimeStats) Snapshot() RuntimeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	observations := make(map[string][]APIObservation, len(s.apiObservations))
	for service, items := range s.apiObservations {
		observations[service] = append([]APIObservation(nil), items...)
	}
	return RuntimeSnapshot{
		StartTime:         s.StartTime,
		PollCount:         s.PollCount,
		LastPollTime:      s.LastPollTime,
		LastPollTaskCount: s.LastPollTaskCount,
		LastPollDuration:  s.LastPollDuration,
		LastPollErr:       s.LastPollErr,
		SendSuccessTotal:  s.SendSuccessTotal,
		SendFailureTotal:  s.SendFailureTotal,
		LastSendTime:      s.LastSendTime,
		APIObservations:   observations,
	}
}
