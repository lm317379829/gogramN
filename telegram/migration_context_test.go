package telegram

import (
	"context"
	"errors"
	"testing"
)

func TestClientPingContextCanceled(t *testing.T) {
	c, err := NewClient(ClientConfig{AppID: 1, AppHash: "test", MemorySession: true, DisableCache: true, NoUpdates: true, NoPreconnect: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Terminate()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.PingContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
