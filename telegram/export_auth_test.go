package telegram

import (
	"context"
	"errors"
	"testing"
)

func TestExportAuthorizationCached(t *testing.T) {
	want := &AuthExportedAuthorization{ID: 42, Bytes: []byte{1}}
	c := &Client{exportedKeys: map[int]*AuthExportedAuthorization{2: want}}
	got, err := c.getExportedAuthorization(context.Background(), 2)
	if err != nil || got != want {
		t.Fatalf("got %v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.getExportedAuthorization(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cache lookup: %v", err)
	}
}
func TestExportAuthorizationWaitCanceled(t *testing.T) {
	pending := &exportAuthPending{done: make(chan struct{})}
	c := &Client{exportingKeys: map[int]*exportAuthPending{2: pending}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.getExportedAuthorization(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestExportAuthorizationInvalidation(t *testing.T) {
	old := &AuthExportedAuthorization{ID: 42, Bytes: []byte{1}}
	fresh := &AuthExportedAuthorization{ID: 43, Bytes: []byte{2}}
	c := &Client{exportedKeys: map[int]*AuthExportedAuthorization{2: old}}
	c.invalidateExportedAuthorization(2, old)
	if c.exportedKeys[2] != nil {
		t.Fatal("rejected auth cached")
	}
	c.exportedKeys[2] = fresh
	c.invalidateExportedAuthorization(2, old)
	if c.exportedKeys[2] != fresh {
		t.Fatal("late failure invalidated new auth")
	}
}

func TestExportAuthorizationCreatorCancelDoesNotCancelFollower(t *testing.T) {
	pending := &exportAuthPending{done: make(chan struct{})}
	c := &Client{exportingKeys: map[int]*exportAuthPending{2: pending}}
	ctx := &observedWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	done := make(chan error, 1)
	want := &AuthExportedAuthorization{ID: 42, Bytes: []byte{1}}
	go func() {
		got, err := c.getExportedAuthorization(ctx, 2)
		if err == nil && got != want {
			err = errors.New("cached auth lost")
		}
		done <- err
	}()
	<-ctx.waiting
	c.exportedKeysMu.Lock()
	c.exportedKeys = map[int]*AuthExportedAuthorization{2: want}
	pending.err = context.Canceled
	delete(c.exportingKeys, 2)
	close(pending.done)
	c.exportedKeysMu.Unlock()
	if err := <-done; err != nil {
		t.Fatalf("active waiter inherited cancellation: %v", err)
	}
}
