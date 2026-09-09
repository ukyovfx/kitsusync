package main

import (
	"errors"
	"net/http"
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
	makeKitsuResponse = func(config.Config) ([]kitsu.MessagePayload, error) {
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
	makeKitsuResponse = func(config.Config) ([]kitsu.MessagePayload, error) {
		return []kitsu.MessagePayload{}, nil
	}

	runOnePoll(config.Config{}, nil)
	if got := setup.Stats.LastPollError(); got != "" {
		t.Fatalf("empty successful poll recorded failure: %q", got)
	}
}
