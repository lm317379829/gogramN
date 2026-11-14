package telegram

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrWorkerTCPDead allows an external scheduler to replace a failed download pool.
// Cancellation is always returned as a context error instead.
var ErrWorkerTCPDead = errors.New("worker TCP link dead")

type downloadSenderRef struct {
	dc       int
	sender   *ExSender
	refs     int
	lastUsed time.Time
	stop     sync.Once
}

func (r *downloadSenderRef) terminate() {
	r.stop.Do(func() {
		if r.sender != nil && r.sender.MTProto != nil {
			_ = r.sender.Terminate()
		}
	})
}

type downloadSenderPending struct {
	done chan struct{}
	err  error
}
type downloadSenderCache struct {
	sync.Mutex
	senders map[int]*downloadSenderRef
	all     map[*downloadSenderRef]struct{}
	pending map[int]*downloadSenderPending
	ctx     context.Context
	cancel  context.CancelFunc
	factory func(context.Context, int) (*ExSender, error)
	closed  bool
}

func newDownloadSenderCache(factory func(context.Context, int) (*ExSender, error)) *downloadSenderCache {
	ctx, cancel := context.WithCancel(context.Background())
	sc := &downloadSenderCache{senders: make(map[int]*downloadSenderRef), all: make(map[*downloadSenderRef]struct{}), pending: make(map[int]*downloadSenderPending), ctx: ctx, cancel: cancel, factory: factory}
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				sc.cleanupIdle(now)
			}
		}
	}()
	return sc
}
func (sc *downloadSenderCache) isClosed() bool { sc.Lock(); defer sc.Unlock(); return sc.closed }
func (sc *downloadSenderCache) acquire(ctx context.Context, dc int) (*downloadSenderRef, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sc.Lock()
		if sc.closed {
			sc.Unlock()
			return nil, errors.New("download sender cache closed")
		}
		if ref := sc.senders[dc]; ref != nil {
			if ref.sender != nil && ref.sender.MTProto != nil && ref.sender.IsTcpActive() {
				ref.refs++
				ref.lastUsed = time.Now()
				sc.Unlock()
				return ref, nil
			}
			delete(sc.senders, dc)
			if ref.refs == 0 {
				delete(sc.all, ref)
				sc.Unlock()
				ref.terminate()
				continue
			}
		}
		if pending := sc.pending[dc]; pending != nil {
			sc.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-sc.ctx.Done():
				return nil, errors.New("download sender cache closed")
			case <-pending.done:
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if pending.err != nil {
				// Only this caller's context may cancel this caller. A canceled
				// creator relinquishes the setup slot to remaining live waiters.
				if errors.Is(pending.err, context.Canceled) || errors.Is(pending.err, context.DeadlineExceeded) {
					continue
				}
				return nil, pending.err
			}
			continue
		}
		pending := &downloadSenderPending{done: make(chan struct{})}
		sc.pending[dc] = pending
		sc.Unlock()
		setup, cancel := context.WithCancel(ctx)
		stop := context.AfterFunc(sc.ctx, cancel)
		sender, err := sc.factory(setup, dc)
		if setup.Err() != nil {
			err = setup.Err()
		}
		stop()
		cancel()
		if err == nil && (sender == nil || sender.MTProto == nil) {
			err = errors.New("nil download sender")
		}
		sc.Lock()
		if sc.closed && err == nil {
			err = errors.New("download sender cache closed")
		}
		var ref *downloadSenderRef
		if err == nil {
			ref = &downloadSenderRef{dc: dc, sender: sender, refs: 1, lastUsed: time.Now()}
			sc.senders[dc] = ref
			sc.all[ref] = struct{}{}
		}
		pending.err = err
		delete(sc.pending, dc)
		close(pending.done)
		sc.Unlock()
		if err != nil {
			if sender != nil && sender.MTProto != nil {
				_ = sender.Terminate()
			}
			return nil, err
		}
		return ref, nil
	}
}
func (sc *downloadSenderCache) release(ref *downloadSenderRef) {
	if ref == nil {
		return
	}
	sc.Lock()
	if ref.refs > 0 {
		ref.refs--
	}
	ref.lastUsed = time.Now()
	retire := ref.refs == 0 && sc.senders[ref.dc] != ref
	if retire {
		delete(sc.all, ref)
	}
	sc.Unlock()
	if retire {
		ref.terminate()
	}
}
func (sc *downloadSenderCache) cleanupIdle(now time.Time) {
	var retired []*downloadSenderRef
	sc.Lock()
	for dc, r := range sc.senders {
		if r.refs == 0 && now.Sub(r.lastUsed) > 30*time.Minute {
			delete(sc.senders, dc)
			delete(sc.all, r)
			retired = append(retired, r)
		}
	}
	sc.Unlock()
	for _, r := range retired {
		r.terminate()
	}
}
func (sc *downloadSenderCache) Close() {
	sc.Lock()
	if sc.closed {
		sc.Unlock()
		return
	}
	sc.closed = true
	sc.cancel()
	refs := make([]*downloadSenderRef, 0, len(sc.all))
	for r := range sc.all {
		refs = append(refs, r)
	}
	clear(sc.senders)
	clear(sc.all)
	sc.Unlock()
	for _, r := range refs {
		r.terminate()
	}
}
func (c *Client) resetDownloadSenders() {
	c.downloadCacheMu.Lock()
	defer c.downloadCacheMu.Unlock()
	if c.downloadSenders == nil || c.downloadSenders.isClosed() {
		c.downloadSenders = newDownloadSenderCache(func(ctx context.Context, dc int) (*ExSender, error) {
			conn, err := c.CreateExportedSender(ctx, dc, false, false)
			if err != nil {
				return nil, err
			}
			return NewExSender(conn), nil
		})
	}
}
func (c *Client) closeDownloadSenders() {
	c.downloadCacheMu.Lock()
	sc := c.downloadSenders
	c.downloadCacheMu.Unlock()
	if sc != nil {
		sc.Close()
	}
}

// NewDownloadPool returns a caller-owned pool referencing a client-owned
// connection shared by DC. Close releases the reference, not another request.
func (c *Client) NewDownloadPool(dc int32, contexts ...context.Context) (*WorkerPool, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if dc == 0 {
		dc = int32(c.GetDC())
	}
	if dc < 1 || dc > 5 {
		return nil, fmt.Errorf("invalid download DC: %d", dc)
	}
	c.downloadCacheMu.Lock()
	sc := c.downloadSenders
	c.downloadCacheMu.Unlock()
	if sc == nil {
		return nil, errors.New("download sender cache unavailable")
	}
	ref, err := sc.acquire(ctx, int(dc))
	if err != nil {
		return nil, err
	}
	pool := NewWorkerPool(1)
	pool.release = func() { sc.release(ref) }
	pool.AddWorker(ref.sender)
	return pool, nil
}
