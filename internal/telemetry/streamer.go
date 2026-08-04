package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

type LogEvent struct {
	Timestamp  time.Time      `json:"timestamp"`
	Level      string         `json:"level"`
	Message    string         `json:"message"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type LogStreamer struct {
	subscribers map[chan string]struct{}
	mu          sync.RWMutex
}

func NewLogStreamer() *LogStreamer {
	return &LogStreamer{
		subscribers: make(map[chan string]struct{}),
	}
}

func (ls *LogStreamer) Subscribe() chan string {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	ch := make(chan string, 100)
	ls.subscribers[ch] = struct{}{}
	return ch
}

func (ls *LogStreamer) Unsubscribe(ch chan string) {
	ls.mu.Lock()
	defer ls.mu.Unlock()
	if _, ok := ls.subscribers[ch]; ok {
		delete(ls.subscribers, ch)
		close(ch)
	}
}

func (ls *LogStreamer) Broadcast(msg string) {
	ls.mu.RLock()
	defer ls.mu.RUnlock()
	for ch := range ls.subscribers {
		select {
		case ch <- msg:
		default:
			// Non-blocking push: drop frame if slow client buffer overflows
		}
	}
}

type streamerHandler struct {
	streamer *LogStreamer
	attrs    []slog.Attr
}

func newStreamerHandler(ls *LogStreamer) slog.Handler {
	return &streamerHandler{
		streamer: ls,
	}
}

func (h *streamerHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *streamerHandler) Handle(_ context.Context, r slog.Record) error {
	attrsMap := make(map[string]any)
	for _, a := range h.attrs {
		attrsMap[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		attrsMap[a.Key] = a.Value.Any()
		return true
	})

	event := LogEvent{
		Timestamp:  r.Time,
		Level:      r.Level.String(),
		Message:    r.Message,
		Attributes: attrsMap,
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	h.streamer.Broadcast(string(data))
	return nil
}

func (h *streamerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := append(make([]slog.Attr, 0, len(h.attrs)+len(attrs)), h.attrs...)
	newAttrs = append(newAttrs, attrs...)
	return &streamerHandler{
		streamer: h.streamer,
		attrs:    newAttrs,
	}
}

func (h *streamerHandler) WithGroup(_ string) slog.Handler {
	return h
}
