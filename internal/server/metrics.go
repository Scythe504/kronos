package server

import (
	"fmt"
	"net/http"
)

// streamLogs SSE handler streams real-time log details and metric events to connected clients.
func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := s.tel.SubscribeLogs()
	defer s.tel.UnsubscribeLogs(ch)

	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\",\"service\":\"%s\"}\n\n", s.tel.GetServiceName())
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
