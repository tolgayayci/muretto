package events

import (
	"testing"

	"github.com/tolgayayci/muretto/server/internal/store"
)

func TestHubFanOutAndSlowConsumer(t *testing.T) {
	h := NewHub()
	a, b := h.Subscribe(), h.Subscribe()
	h.Publish([]store.Event{{Seq: 1, Type: "resource"}})
	if e := <-a.C; e.Seq != 1 {
		t.Fatalf("a got %+v", e)
	}
	if e := <-b.C; e.Seq != 1 {
		t.Fatalf("b got %+v", e)
	}
	// Overfill b's buffer while a keeps draining.
	for i := range SubBuffer + 1 {
		h.Publish([]store.Event{{Seq: int64(i + 2)}})
		<-a.C
	}
	n := 0
	for range b.C {
		n++
	}
	if n != SubBuffer || h.Subscribers() != 1 {
		t.Fatalf("slow consumer: drained %d, subscribers %d", n, h.Subscribers())
	}
	h.Unsubscribe(a)
	h.Unsubscribe(a) // idempotent
	if _, ok := <-a.C; ok || h.Subscribers() != 0 {
		t.Fatal("unsubscribe did not close")
	}
}
