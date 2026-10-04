package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ymotongpoo/reviewer/internal/app"
)

func TestHubClosesFullSubscriber(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	for i := 1; i <= 65; i++ {
		h.Publish(app.Event{Type: "round", Round: i})
	}
	for i := 1; i <= 64; i++ {
		select {
		case ev, ok := <-ch:
			if !ok || ev.Type != "round" || ev.Round != i {
				t.Fatalf("event %d = %+v, open = %v", i, ev, ok)
			}
		default:
			t.Fatalf("missing buffered event %d", i)
		}
	}
	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("received event after overflow: %+v", ev)
		}
	default:
		t.Fatal("full subscriber was not closed")
	}
	if len(h.subs) != 0 {
		t.Fatal("full subscriber is still registered")
	}
	h.Publish(app.Event{Type: "round", Round: 66})
	h.Unsubscribe(ch)
	h.Unsubscribe(ch)
}

func TestHubKeepsOtherSubscribers(t *testing.T) {
	h := NewHub()
	slow := h.Subscribe()
	fast := h.Subscribe()
	defer h.Unsubscribe(slow)
	defer h.Unsubscribe(fast)
	for i := 1; i <= 66; i++ {
		h.Publish(app.Event{Type: "round", Round: i})
		select {
		case ev, ok := <-fast:
			if !ok || ev.Type != "round" || ev.Round != i {
				t.Fatalf("fast subscriber event %d = %+v, open = %v", i, ev, ok)
			}
		default:
			t.Fatalf("fast subscriber missed event %d", i)
		}
	}
	if len(h.subs) != 1 || !h.subs[fast] {
		t.Fatal("only the fast subscriber should remain registered")
	}
	for i := 0; i < 64; i++ {
		select {
		case _, ok := <-slow:
			if !ok {
				t.Fatal("slow subscriber closed before buffered events were drained")
			}
		default:
			t.Fatal("slow subscriber lost a buffered event")
		}
	}
	select {
	case _, ok := <-slow:
		if ok {
			t.Fatal("slow subscriber received an event after overflow")
		}
	default:
		t.Fatal("slow subscriber was not closed")
	}
}

func TestHubUnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	h.Unsubscribe(ch)
	h.Unsubscribe(ch)
	h.Unsubscribe(nil)
	h.Unsubscribe(make(chan app.Event))
	h.Publish(app.Event{Type: "comments"})
	if len(h.subs) != 0 {
		t.Fatal("unsubscribed channel is still registered")
	}
	select {
	case ev, ok := <-ch:
		t.Fatalf("Unsubscribe closed the channel or Publish delivered an event: %+v, open = %v", ev, ok)
	default:
	}
}

func TestHubConcurrentPublishAndUnsubscribe(t *testing.T) {
	h := NewHub()
	for i := 0; i < 100; i++ {
		ch := h.Subscribe()
		for j := 0; j < 64; j++ {
			h.Publish(app.Event{Type: "comments"})
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func(publish bool) {
				defer wg.Done()
				<-start
				if publish {
					h.Publish(app.Event{Type: "comments"})
				} else {
					h.Unsubscribe(ch)
				}
			}(j%2 == 0)
		}
		close(start)
		wg.Wait()
		if len(h.subs) != 0 {
			t.Fatal("subscriber is still registered after concurrent removal")
		}
	}
}

// The first write happens after Subscribe, before the handler can drain events.
type blockedEventsWriter struct {
	*httptest.ResponseRecorder
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *blockedEventsWriter) Write(b []byte) (int, error) {
	w.once.Do(func() {
		close(w.started)
		<-w.release
	})
	return w.ResponseRecorder.Write(b)
}

func TestEventsStreamEndsOnOverflow(t *testing.T) {
	p := &Project{Hub: NewHub()}
	w := &blockedEventsWriter{
		ResponseRecorder: httptest.NewRecorder(),
		started:          make(chan struct{}),
		release:          make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest("GET", "/api/events", nil).WithContext(ctx)
	done := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(w.release) }) }
	t.Cleanup(func() {
		cancel()
		release()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("events handler did not stop during cleanup")
		}
	})
	go func() {
		defer close(done)
		p.handleEvents(w, r)
	}()
	select {
	case <-w.started:
	case <-time.After(5 * time.Second):
		t.Fatal("events handler did not start writing")
	}
	for i := 1; i <= 65; i++ {
		p.Hub.Publish(app.Event{Type: "round", Round: i})
	}
	release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("events handler did not return after overflow")
	}
	if len(p.Hub.subs) != 0 {
		t.Fatal("events subscriber is still registered")
	}
	for key, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	} {
		if got := w.Header().Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if w.Code != 200 || !w.Flushed {
		t.Errorf("status = %d, flushed = %v", w.Code, w.Flushed)
	}
	var want strings.Builder
	want.WriteString("retry: 2000\n\n")
	for i := 1; i <= 64; i++ {
		fmt.Fprintf(&want, "data: {\"type\":\"round\",\"round\":%d}\n\n", i)
	}
	if got := w.Body.String(); got != want.String() {
		t.Errorf("SSE body = %q, want %q", got, want.String())
	}
}
