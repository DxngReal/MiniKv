package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"minikv/internal/engine"
	"minikv/internal/kverrors"
	"minikv/internal/version"
)

// defaultLogger discards output; used when no logger is configured.
var defaultLogger = slog.New(slog.NewJSONHandler(io.Discard, nil))

// maxBodyBytes caps request bodies (JSON overhead + 1 MiB value limit).
const maxBodyBytes = 1 << 21

// SnapshotFileWriter is implemented by engines that can persist a snapshot
// file (the durable engine). The API depends on this narrow interface, not
// on the concrete type.
type SnapshotFileWriter interface {
	WriteSnapshot() (engine.SnapshotStats, error)
}

// errorBody is the consistent JSON error shape.
type errorBody struct {
	Error   string `json:"error"`   // machine-readable kind, e.g. "key_not_found"
	Message string `json:"message"` // what happened and why (no key/value contents)
	Hint    string `json:"hint"`    // what the caller can do next
}

// writeError maps a MiniKV error to the consistent JSON error response.
func writeError(w http.ResponseWriter, err error) {
	kind, ok := kverrors.KindOf(err)
	if !ok {
		kind = kverrors.ServerFailure
	}
	status := http.StatusInternalServerError
	switch kind {
	case kverrors.KeyNotFound:
		status = http.StatusNotFound
	case kverrors.InvalidKey, kverrors.InvalidValue, kverrors.InvalidTTL:
		status = http.StatusBadRequest
	case kverrors.StoreClosed, kverrors.ServerFailure:
		status = http.StatusServiceUnavailable
	case kverrors.WALCorruption, kverrors.RecoveryFailure, kverrors.DataDirFailure, kverrors.SnapshotFailure:
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{
		Error:   string(kind),
		Message: err.Error(),
		Hint:    kind.Hint(),
	})
}

// writeJSON writes a 2xx JSON response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// routes builds the mux. Go 1.22 pattern routing handles methods and the
// {key} wildcard.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /v1/keys/{key}", s.handleSet)
	mux.HandleFunc("GET /v1/keys/{key}", s.handleGet)
	mux.HandleFunc("DELETE /v1/keys/{key}", s.handleDelete)
	mux.HandleFunc("GET /v1/keys", s.handleKeys)
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("POST /v1/snapshot", s.handleSnapshot)
	return mux
}

// setResponse reports whether a PUT created a new key.
type setResponse struct {
	Key     string `json:"key"`
	Created bool   `json:"created"`
}

// handleSet implements PUT /v1/keys/{key}.
func (s *Server) handleSet(w http.ResponseWriter, r *http.Request) {
	const op = "api.handleSet"
	key := r.PathValue("key")

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		writeError(w, kverrors.New(kverrors.InvalidValue, op,
			"could not read the request body"))
		return
	}
	if len(body) > maxBodyBytes {
		writeError(w, kverrors.New(kverrors.InvalidValue, op,
			"request body exceeds %d bytes", maxBodyBytes))
		return
	}

	var req struct {
		Value *string `json:"value"`
		TTLMS *int64  `json:"ttl_ms"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, kverrors.New(kverrors.InvalidValue, op,
			"malformed JSON body: the request must be {\"value\": \"...\", \"ttl_ms\": 60000}"))
		return
	}
	if req.Value == nil {
		writeError(w, kverrors.New(kverrors.InvalidValue, op,
			`the body must contain a "value" field`))
		return
	}

	var expiresAt *time.Time
	if req.TTLMS != nil {
		if *req.TTLMS < 0 {
			writeError(w, kverrors.New(kverrors.InvalidTTL, op,
				"ttl_ms must not be negative"))
			return
		}
		if *req.TTLMS > 0 {
			t := time.Now().Add(time.Duration(*req.TTLMS) * time.Millisecond)
			expiresAt = &t
		}
	}

	existed, err := s.eng.Set(key, []byte(*req.Value), expiresAt)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if !existed {
		status = http.StatusCreated
	}
	writeJSON(w, status, setResponse{Key: key, Created: !existed})
}

// handleGet implements GET /v1/keys/{key}.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	value, err := s.eng.Get(key)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"key":   key,
		"value": string(value),
	})
}

// handleDelete implements DELETE /v1/keys/{key}.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	existed, err := s.eng.Delete(key)
	if err != nil {
		writeError(w, err)
		return
	}
	if !existed {
		writeError(w, kverrors.New(kverrors.KeyNotFound, "api.handleDelete",
			"key does not exist"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleKeys implements GET /v1/keys. Only key names are exposed.
func (s *Server) handleKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.eng.Keys()
	if err != nil {
		writeError(w, err)
		return
	}
	if keys == nil {
		keys = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "count": len(keys)})
}

// statusResponse is the /v1/status payload required by the specification.
type statusResponse struct {
	Version         string  `json:"version"`
	UptimeSeconds   int64   `json:"uptime_seconds"`
	KeyCount        int64   `json:"key_count"`
	WALBytes        int64   `json:"wal_bytes"`
	SnapshotEntries int64   `json:"snapshot_entries"`
	DurabilityMode  string  `json:"durability_mode"`
	ShardCount      int     `json:"shard_count"`
	HitRate         float64 `json:"hit_rate"`
}

// handleStatus implements GET /v1/status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st := s.eng.Stats()
	writeJSON(w, http.StatusOK, statusResponse{
		Version:         version.Version,
		UptimeSeconds:   int64(time.Since(s.started).Seconds()),
		KeyCount:        st.KeyCount,
		WALBytes:        st.WALBytes,
		SnapshotEntries: st.SnapshotEntries,
		DurabilityMode:  st.DurabilityMode,
		ShardCount:      st.ShardCount,
		HitRate:         st.HitRate(),
	})
}

// snapshotResponse reports the outcome of a snapshot write.
type snapshotResponse struct {
	Entries    int    `json:"entries"`
	Bytes      int64  `json:"bytes"`
	DurationMS int64  `json:"duration_ms"`
	Path       string `json:"path"`
}

// handleSnapshot implements POST /v1/snapshot via the durable engine.
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	sw, ok := s.eng.(SnapshotFileWriter)
	if !ok {
		writeError(w, kverrors.New(kverrors.ServerFailure, "api.handleSnapshot",
			"this engine does not support writing snapshot files"))
		return
	}
	stats, err := sw.WriteSnapshot()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snapshotResponse{
		Entries:    stats.Entries,
		Bytes:      stats.Bytes,
		DurationMS: stats.Duration.Milliseconds(),
		Path:       stats.Path,
	})
}
