package main

import (
	"errors"
	"testing"

	"app/src/api/kitsu"
	"app/src/setup"
	"app/src/utils/config"
)

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
