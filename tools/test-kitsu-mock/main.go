package main

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func main() {
	http.HandleFunc("/api/data/projects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, []map[string]string{{"id": "test-production", "name": "Test Production"}})
	})
	http.HandleFunc("/api/data/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, []map[string]string{{"id": "test-production", "name": "Test Production"}})
	})
	http.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, []any{})
	})
	if err := http.ListenAndServe(":80", nil); err != nil {
		panic(err)
	}
}
