package main

import (
	"context"
	"errors"
	"log"
	"net/http"

	accountmonitor "github.com/ListenCodes/sub2api-account-monitor"
)

type publicGroupReader interface {
	ReadPublicGroups(context.Context) ([]accountmonitor.PublicGroup, error)
}

func (s *HTTPServer) handlePublicGroups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	if s.publicGroups == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("public groups unavailable"))
		return
	}
	items, err := s.publicGroups.ReadPublicGroups(r.Context())
	if err != nil {
		log.Printf("read public groups: %v", err)
		writeError(w, http.StatusServiceUnavailable, errors.New("public groups unavailable"))
		return
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": items})
}
