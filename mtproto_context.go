package gogram

import (
	"context"
	"errors"
	"github.com/xAmarnath/gogram/internal/utils"
	"time"
)

// handleFloodWait preserves the original callback while allowing cancellable waits.
func (m *MTProto) handleFloodWait(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if m.floodHandlerContext != nil {
		return m.floodHandlerContext(ctx, err) && ctx.Err() == nil
	}
	return m.floodHandler != nil && m.floodHandler(err) && ctx.Err() == nil
}

// PingContext sends a ping on the current connection and returns send latency.
// Like legacy Ping, it does not wait for Pong and does not measure network RTT.
// Request cancellation does not force-close the shared transport; a blocked
// transport write remains subject to the connection's own write deadline.
func (m *MTProto) PingContext(ctx context.Context) (time.Duration, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !m.IsTcpActive() {
		return 0, errors.New("ping: transport unavailable")
	}
	start := time.Now()
	if err := m.InvokeRequestWithoutUpdate(ctx, &utils.PingParams{PingID: time.Now().UnixNano()}); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}
