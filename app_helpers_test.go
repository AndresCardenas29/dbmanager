package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"dbmanager/internal/services"
	"dbmanager/internal/store"
)

// evRec es un evento de progreso capturado.
type evRec struct {
	Event string
	P     services.Progress
}

type evLog struct {
	mu  sync.Mutex
	evs []evRec
}

func (l *evLog) add(ev string, p services.Progress) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evs = append(l.evs, evRec{ev, p})
}

func (l *evLog) of(event string) []services.Progress {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []services.Progress
	for _, e := range l.evs {
		if e.Event == event {
			out = append(out, e.P)
		}
	}
	return out
}

func (l *evLog) reset() {
	l.mu.Lock()
	l.evs = nil
	l.mu.Unlock()
}

// newTestApp construye un App sin Wails: store en un directorio temporal, contexto de fondo y un
// servicio con un emisor propio (NUNCA wruntime.EventsEmit, que hace log.Fatalf fuera de Wails).
func newTestApp(t testing.TB) (*App, *evLog) {
	t.Helper()
	// No se usa t.TempDir: en Windows SQLite puede mantener bloqueado el WAL unos instantes tras cerrar.
	dir, err := os.MkdirTemp("", "dbm-app-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 10; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	t.Setenv("DBMANAGER_DATA_DIR", dir)
	a := NewApp()
	a.ctx = context.Background()
	// NewApp cablea emit a wruntime.EventsEmit (log.Fatalf fuera de Wails): se reemplaza por un no-op.
	a.emit = func(string, any) {}
	a.store, a.initErr = store.Open()
	if a.initErr != nil {
		t.Fatal(a.initErr)
	}
	log := &evLog{}
	a.svcOnce.Do(func() {
		a.svc = &services.Services{Store: a.store, Docker: a.dockerClient, Emit: log.add}
	})
	t.Cleanup(func() { a.shutdown(nil) })
	return a, log
}

func mustJSON(t testing.TB, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func noSecret(t testing.TB, what, text string, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if s != "" && strings.Contains(text, s) {
			t.Errorf("%s contiene un secreto (%q): %.300s", what, s, text)
		}
	}
}

func servicesProgress(p ProgressEvent) services.Progress {
	return services.Progress{ID: p.ID, RequestID: p.RequestID, Percent: p.Percent, Message: p.Message, Done: p.Done, Error: p.Error}
}
