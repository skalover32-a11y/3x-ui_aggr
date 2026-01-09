package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Version string `json:"version,omitempty"`
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", withAuth(healthHandler))
	mux.HandleFunc("/version", withAuth(versionHandler))
	mux.HandleFunc("/ops/reboot", withAuth(rebootHandler))
	mux.HandleFunc("/ops/update", withAuth(updateHandler))

	addr := os.Getenv("NODE_AGENT_ADDR")
	if addr == "" {
		addr = ":9090"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("node-agent listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !allowIP(r.RemoteAddr) {
			writeJSON(w, http.StatusForbidden, response{OK: false, Message: "forbidden"})
			return
		}
		token := strings.TrimSpace(os.Getenv("NODE_AGENT_TOKEN"))
		if token != "" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			if auth != "Bearer "+token {
				writeJSON(w, http.StatusUnauthorized, response{OK: false, Message: "unauthorized"})
				return
			}
		}
		next(w, r)
	}
}

func allowIP(remote string) bool {
	allow := strings.TrimSpace(os.Getenv("NODE_AGENT_ALLOWLIST"))
	if allow == "" {
		return true
	}
	ip := remote
	if host, _, err := net.SplitHostPort(remote); err == nil {
		ip = host
	}
	parts := strings.Split(allow, ",")
	for _, part := range parts {
		if strings.TrimSpace(part) == ip {
			return true
		}
	}
	return false
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, response{OK: true})
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	version := strings.TrimSpace(os.Getenv("NODE_AGENT_VERSION"))
	if version == "" {
		version = "dev"
	}
	writeJSON(w, http.StatusOK, response{OK: true, Version: version})
}

func rebootHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sudo", "/sbin/reboot")
	if err := cmd.Start(); err != nil {
		writeJSON(w, http.StatusBadRequest, response{OK: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, response{OK: true})
}

func updateHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, response{OK: false, Message: "not implemented"})
}

func writeJSON(w http.ResponseWriter, status int, payload response) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
