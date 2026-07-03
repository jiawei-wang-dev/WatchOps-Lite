package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type serialSSEWriter struct {
	ctx     context.Context
	writer  http.ResponseWriter
	flusher http.Flusher
	mu      sync.Mutex
	failed  bool
}

func newSerialSSEWriter(
	ctx context.Context,
	writer http.ResponseWriter,
	flusher http.Flusher,
) *serialSSEWriter {
	return &serialSSEWriter{
		ctx:     ctx,
		writer:  writer,
		flusher: flusher,
	}
}

func (w *serialSSEWriter) Write(eventType string, data any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed || w.ctx.Err() != nil {
		return
	}
	if err := writeSSE(w.writer, w.flusher, eventType, data); err != nil {
		w.failed = true
	}
}

func writeSSE(
	w http.ResponseWriter,
	flusher http.Flusher,
	eventType string,
	data any,
) error {
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		w,
		"event: %s\ndata: %s\n\n",
		eventType,
		encoded,
	); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}
