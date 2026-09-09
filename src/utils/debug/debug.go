package debug

import (
	"app/src/utils/config"
	"log/slog"
	"net/http"
	"runtime"
)

func Info(resp *http.Response, respBody []byte) {

	if config.Read().Debug == true {
		// Func name and path
		pc := make([]uintptr, 10) // at least 1 entry needed
		runtime.Callers(2, pc)
		f := runtime.FuncForPC(pc[0])
		file, line := f.FileLine(pc[0])
		slog.Info("debug response", "file", file, "line", line, "func", f.Name())

		// Headers and bodies can include credentials, cookies, and personal data.
		// Keep debug output useful without serializing response content.
		slog.Info("debug response metadata", "status", resp.StatusCode, "body_bytes", len(respBody))
	}
}
