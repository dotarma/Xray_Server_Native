package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type requestIDKey struct{}

func init() {
	log.SetFlags(0)
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (w *responseRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := randomHex(12)
		if requestID == "" {
			requestID = fmt.Sprintf("fallback-%x", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", requestID)
		started := time.Now()
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		logEvent("http_request", map[string]any{
			"durationMs": time.Since(started).Milliseconds(),
			"method":     r.Method,
			"path":       r.URL.Path,
			"requestId":  requestID,
			"status":     status,
		})
	})
}

func requestIDFromContext(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func logEvent(event string, fields map[string]any) {
	fields["event"] = event
	fields["timestamp"] = time.Now().UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(fields)
	if err != nil {
		log.Printf(`{"event":"log_encode_error"}`)
		return
	}
	log.Print(string(encoded))
}
