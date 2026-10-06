package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/example/devtools-release-gate/rollout"
)

type service struct {
	flags   *rollout.FlagsClient
	gate    *rollout.Gate
	flagKey string
}

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	flagKey := envOr("DEVTOOLS_FLAG_KEY", "devtools.symbol-index")
	flags := rollout.NewFlagsClient(rollout.DefaultInfraiBaseURL, apiKey, nil)
	service := &service{flags: flags, gate: rollout.NewGate(flags), flagKey: flagKey}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /build-events", service.buildEvent)
	mux.HandleFunc("POST /release-operations", service.releaseOperation)
	mux.HandleFunc("GET /diagnostics", service.diagnostics)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	address := envOr("LISTEN_ADDR", ":8080")
	log.Printf("release gate listening on %s for %s", address, flagKey)
	log.Fatal(http.ListenAndServe(address, mux))
}

func (s *service) buildEvent(w http.ResponseWriter, r *http.Request) {
	var event rollout.BuildEvent
	if err := decodeJSON(r, &event); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	decision, err := s.gate.Decide(r.Context(), s.flagKey, event)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *service) releaseOperation(w http.ResponseWriter, r *http.Request) {
	var operation rollout.ReleaseOperation
	if err := decodeJSON(r, &operation); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if operation.FlagKey == "" {
		operation.FlagKey = s.flagKey
	}
	if err := rollout.ApplyRelease(r.Context(), s.flags, operation); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"operation_id": operation.OperationID,
		"flag_key":     operation.FlagKey,
		"percentage":   operation.Percentage,
		"status":       "applied",
	})
}

func (s *service) diagnostics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"decisions": s.gate.Diagnostics()})
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeServiceError(w http.ResponseWriter, err error) {
	var infraiErr *rollout.InfraiError
	if errors.As(err, &infraiErr) && infraiErr.HTTPStatus >= 400 && infraiErr.HTTPStatus < 500 {
		writeJSON(w, infraiErr.HTTPStatus, map[string]string{"error": infraiErr.Message, "code": infraiErr.Code})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
