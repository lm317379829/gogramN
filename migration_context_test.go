package gogram

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPingContextCanceled(t *testing.T) {
	m, err := NewMTProto(Config{MemorySession: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = m.PingContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
func TestPingContextInactive(t *testing.T) {
	m, err := NewMTProto(Config{MemorySession: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err = m.PingContext(ctx); err == nil {
		t.Fatal("inactive connection reported healthy")
	}
}
func TestFloodHandlerContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	legacy := false
	m, err := NewMTProto(Config{MemorySession: true, FloodHandler: func(error) bool { legacy = true; return true }, FloodHandlerContext: func(c context.Context, e error) bool { cancel(); <-c.Done(); return false }})
	if err != nil {
		t.Fatal(err)
	}
	if m.handleFloodWait(ctx, errors.New("FLOOD_WAIT_60")) {
		t.Fatal("canceled callback retried")
	}
	if legacy {
		t.Fatal("legacy callback shadowed context callback")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatal(ctx.Err())
	}
}
func TestLegacyFloodHandler(t *testing.T) {
	m, err := NewMTProto(Config{MemorySession: true, FloodHandler: func(error) bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if !m.handleFloodWait(context.Background(), errors.New("FLOOD_WAIT_1")) {
		t.Fatal("legacy callback lost")
	}
}
