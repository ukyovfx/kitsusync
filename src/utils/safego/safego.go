// Package safego keeps a single panic in a background task from taking the
// whole process (and the web UI with it) down.
package safego

import (
	"fmt"
	"runtime/debug"

	"github.com/gookit/slog"
	"github.com/robfig/cron/v3"
)

// logPanic records the panic type and stack only. The panic value is left out
// on purpose because it may embed tokens or other secrets.
func logPanic(name string, r interface{}) {
	slog.Error("recovered from panic", "task", name, "panic_type", fmt.Sprintf("%T", r), "stack", string(debug.Stack()))
}

// Run executes fn and swallows (and logs) any panic. It returns true when fn
// completed without panicking.
func Run(name string, fn func()) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logPanic(name, r)
			ok = false
		}
	}()
	fn()
	return true
}

// Go runs fn in a new goroutine with panic recovery.
func Go(name string, fn func()) {
	go Run(name, fn)
}

// CronRecover is a cron.JobWrapper that logs and absorbs panics. Place it
// before cron.DelayIfStillRunning so it is the outermost wrapper.
func CronRecover() cron.JobWrapper {
	return func(j cron.Job) cron.Job {
		return cron.FuncJob(func() { Run("cron job", j.Run) })
	}
}
