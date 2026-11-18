package main

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"app/src/api/kitsu"
	"app/src/setup"
	"app/src/utils/config"
)

func TestApplicationHTTPServerHasBoundedResourceTimeouts(t *testing.T) {
	server := newApplicationHTTPServer(http.NotFoundHandler())
	if server.ReadHeaderTimeout != 10*time.Second ||
		server.ReadTimeout != 30*time.Second ||
		server.WriteTimeout != 90*time.Second ||
		server.IdleTimeout != 120*time.Second ||
		server.MaxHeaderBytes != 32<<10 {
		t.Fatalf("unexpected HTTP resource limits: %+v", server)
	}
}

func TestRunOnePollRecordsFailureAndRecoversOnSuccessfulEmptyPoll(t *testing.T) {
	oldFactory := makeKitsuResponse
	oldStats := setup.Stats
	defer func() {
		makeKitsuResponse = oldFactory
		setup.Stats = oldStats
	}()

	setup.Stats = &setup.RuntimeStats{}
	call := 0
	makeKitsuResponse = func(context.Context, config.Config) ([]kitsu.MessagePayload, error) {
		call++
		if call == 1 {
			return nil, errors.New("Kitsu unavailable")
		}
		return []kitsu.MessagePayload{}, nil
	}

	runOnePoll(config.Config{}, nil)
	if got := setup.Stats.LastPollError(); got == "" {
		t.Fatal("failed Kitsu poll was not recorded")
	}

	runOnePoll(config.Config{}, nil)
	if got := setup.Stats.LastPollError(); got != "" {
		t.Fatalf("successful empty poll retained failure: %q", got)
	}
}

func TestRunOnePollCountsLegitimateEmptyPollAsSuccess(t *testing.T) {
	oldFactory := makeKitsuResponse
	oldStats := setup.Stats
	defer func() {
		makeKitsuResponse = oldFactory
		setup.Stats = oldStats
	}()

	setup.Stats = &setup.RuntimeStats{}
	makeKitsuResponse = func(context.Context, config.Config) ([]kitsu.MessagePayload, error) {
		return []kitsu.MessagePayload{}, nil
	}

	runOnePoll(config.Config{}, nil)
	if got := setup.Stats.LastPollError(); got != "" {
		t.Fatalf("empty successful poll recorded failure: %q", got)
	}
}

// Shutdown must be observable as "no poll is running" before SQLite is closed.
func TestShutdownWaitsForRunningPollBeforeClosingDatabase(t *testing.T) {
	oldFactory := makeKitsuResponse
	oldStats := setup.Stats
	defer func() {
		makeKitsuResponse = oldFactory
		setup.Stats = oldStats
	}()
	setup.Stats = &setup.RuntimeStats{}

	answered := make(chan struct{})
	release := make(chan struct{})
	makeKitsuResponse = func(context.Context, config.Config) ([]kitsu.MessagePayload, error) {
		close(answered)
		<-release
		return []kitsu.MessagePayload{}, nil
	}

	var pollDone atomic.Bool
	trackPoll(func() {
		runOnePoll(config.Config{}, nil)
		pollDone.Store(true)
	})

	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("poll never started")
	}

	// The shutdown sequence: wait for poll work, then close the database.
	waited := make(chan struct{})
	go func() {
		pollWork.Wait()
		close(waited)
	}()

	select {
	case <-waited:
		t.Fatal("shutdown stopped waiting while a poll was still running")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish waiting after the poll returned")
	}
	if !pollDone.Load() {
		t.Fatal("poll goroutine was not finished when the wait returned")
	}
}

func TestRunOnePollContextAbortsWhenShutdownCancelsTheRoot(t *testing.T) {
	oldFactory := makeKitsuResponse
	oldStats := setup.Stats
	defer func() {
		makeKitsuResponse = oldFactory
		setup.Stats = oldStats
	}()
	setup.Stats = &setup.RuntimeStats{}

	seen := make(chan error, 1)
	makeKitsuResponse = func(ctx context.Context, _ config.Config) ([]kitsu.MessagePayload, error) {
		seen <- ctx.Err()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runOnePollContext(ctx, config.Config{}, nil)

	select {
	case err := <-seen:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("poll did not receive the cancelled root context: %v", err)
		}
	default:
		t.Fatal("poll ran without a cancelled root context")
	}
	if got := setup.Stats.LastPollError(); got == "" {
		t.Fatal("cancelled poll was not recorded as a failure")
	}
}
