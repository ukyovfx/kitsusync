package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// registerDocsRoutes serves the read-only static documentation entry point and
// its same-origin JSX dependency. The aliases preserve the paths documented by
// older releases while keeping the current /bot/docs URL usable.
func registerDocsRoutes(mux *http.ServeMux) {
	for _, prefix := range []string{"", "/bot"} {
		mux.HandleFunc(prefix+"/docs", docsHTMLHandler)
		mux.HandleFunc(prefix+"/docs/", docsSubpathHandler)
	}
	// Keep the historical root asset aliases available to existing links.
	mux.HandleFunc("/site.jsx", docsSiteHandler)
}

func docsHTMLHandler(w http.ResponseWriter, r *http.Request) {
	serveDocsAsset(w, r, "docs.html")
}

func docsSubpathHandler(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/site.jsx") {
		docsSiteHandler(w, r)
		return
	}
	// /docs/ and /bot/docs/ are aliases for the entry point.
	docsHTMLHandler(w, r)
}

func docsSiteHandler(w http.ResponseWriter, r *http.Request) {
	serveDocsAsset(w, r, "site.jsx")
}

func serveDocsAsset(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	path, ok := findRuntimeAsset(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func findRuntimeAsset(name string) (string, bool) {
	candidates := []string{filepath.Join(".", name)}
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), name))
	}
	// go test ./src executes with src/ as the package directory; the runtime
	// image and compose service use /app as the working directory.
	candidates = append(candidates, filepath.Join("..", name))
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}
