package safego

import (
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
)

func TestRunRecoversPanic(t *testing.T) {
	if Run("test", func() { panic("secret-token-value") }) {
		t.Fatal("Run must report false when fn panics")
	}
	if !Run("test", func() {}) {
		t.Fatal("Run must report true when fn succeeds")
	}
}

func TestGoRecoversPanic(t *testing.T) {
	done := make(chan struct{})
	Go("test", func() {
		defer close(done)
		panic("boom")
	})
	<-done
	time.Sleep(10 * time.Millisecond) // process must still be alive
}

func TestCronJobPanicKeepsSchedulerAlive(t *testing.T) {
	c := cron.New(cron.WithChain(CronRecover(), cron.DelayIfStillRunning(cron.DiscardLogger)))
	var wg sync.WaitGroup
	wg.Add(2)
	c.Schedule(cron.Every(20*time.Millisecond), cron.FuncJob(func() {
		defer wg.Done()
		panic("job failed")
	}))
	c.Start()
	defer c.Stop()
	// Two runs means the panic in the first run did not kill the process or the scheduler.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cron job did not run again after panicking")
	}
}
