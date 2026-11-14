package telegram

import (
	"bytes"
	"context"
	"errors"
	"fmt"
)

type exportAuthPending struct {
	done chan struct{}
	auth *AuthExportedAuthorization
	err  error
}

// getExportedAuthorization coalesces authorization export per DC. A canceled
// waiter never cancels the caller which is already exporting the authorization.
func (c *Client) getExportedAuthorization(ctx context.Context, dc int32) (*AuthExportedAuthorization, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.exportedKeysMu.Lock()
		if auth := c.exportedKeys[int(dc)]; auth != nil {
			c.exportedKeysMu.Unlock()
			return auth, nil
		}
		if pending := c.exportingKeys[int(dc)]; pending != nil {
			c.exportedKeysMu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-pending.done:
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
				continue
			}
			return pending.auth, pending.err
		}
		if c.exportingKeys == nil {
			c.exportingKeys = make(map[int]*exportAuthPending)
		}
		pending := &exportAuthPending{done: make(chan struct{})}
		c.exportingKeys[int(dc)] = pending
		c.exportedKeysMu.Unlock()
		response, err := c.MakeRequest(ctx, &AuthExportAuthorizationParams{DcID: dc})
		var auth *AuthExportedAuthorization
		if err == nil {
			var ok bool
			auth, ok = response.(*AuthExportedAuthorization)
			if !ok || auth == nil {
				err = fmt.Errorf("unexpected exported authorization: %T", response)
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			auth = nil
		}
		c.exportedKeysMu.Lock()
		if err == nil {
			if c.exportedKeys == nil {
				c.exportedKeys = make(map[int]*AuthExportedAuthorization)
			}
			c.exportedKeys[int(dc)] = auth
		}
		pending.auth, pending.err = auth, err
		delete(c.exportingKeys, int(dc))
		close(pending.done)
		c.exportedKeysMu.Unlock()
		return auth, err
	}
}
func (c *Client) invalidateExportedAuthorization(dc int, rejected *AuthExportedAuthorization) {
	if rejected == nil {
		return
	}
	c.exportedKeysMu.Lock()
	defer c.exportedKeysMu.Unlock()
	if current := c.exportedKeys[dc]; current != nil && current.ID == rejected.ID && bytes.Equal(current.Bytes, rejected.Bytes) {
		delete(c.exportedKeys, dc)
	}
}
