package telegram

import (
	"context"
	"errors"
	mtproto "github.com/xAmarnath/gogram"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func offlineSender(t *testing.T) *ExSender {
	t.Helper()
	m, err := mtproto.NewMTProto(mtproto.Config{MemorySession: true})
	if err != nil {
		t.Fatal(err)
	}
	m.TcpState().SetActive(true)
	return NewExSender(m)
}
func TestDownloadSenderConcurrentAcquire(t *testing.T) {
	var calls atomic.Int32
	sc := newDownloadSenderCache(func(ctx context.Context, dc int) (*ExSender, error) { calls.Add(1); return offlineSender(t), nil })
	defer sc.Close()
	const n = 24
	refs := make(chan *downloadSenderRef, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			r, err := sc.acquire(context.Background(), 2)
			if err != nil {
				t.Error(err)
				return
			}
			refs <- r
		})
	}
	wg.Wait()
	close(refs)
	var first *downloadSenderRef
	for r := range refs {
		if first == nil {
			first = r
		}
		if r != first {
			t.Error("same DC acquired different sender")
		}
		sc.release(r)
	}
	if calls.Load() != 1 {
		t.Fatalf("created %d senders", calls.Load())
	}
}
func TestDownloadSenderWaitCanceled(t *testing.T) {
	entered := make(chan struct{})
	unblock := make(chan struct{})
	sc := newDownloadSenderCache(func(ctx context.Context, dc int) (*ExSender, error) {
		close(entered)
		select {
		case <-unblock:
			return offlineSender(t), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	defer sc.Close()
	done := make(chan error, 1)
	go func() {
		r, e := sc.acquire(context.Background(), 2)
		if r != nil {
			sc.release(r)
		}
		done <- e
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sc.acquire(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	close(unblock)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestDownloadSenderShutdown(t *testing.T) {
	entered := make(chan struct{})
	sc := newDownloadSenderCache(func(ctx context.Context, dc int) (*ExSender, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	done := make(chan error, 1)
	go func() { _, e := sc.acquire(context.Background(), 2); done <- e }()
	<-entered
	sc.Close()
	sc.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("creation survived shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel setup")
	}
	if _, err := sc.acquire(context.Background(), 2); err == nil {
		t.Fatal("closed cache created sender")
	}
}
func TestDownloadPoolCloseIdempotent(t *testing.T) {
	sc := newDownloadSenderCache(func(context.Context, int) (*ExSender, error) { return offlineSender(t), nil })
	defer sc.Close()
	ref, err := sc.acquire(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	pool := NewWorkerPool(1)
	pool.release = func() { sc.release(ref) }
	pool.AddWorker(ref.sender)
	pool.Close()
	pool.Close()
	if ref.refs != 0 {
		t.Fatalf("leaked %d references", ref.refs)
	}
	next, err := sc.acquire(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.release(next)
	if next != ref || !next.sender.IsTcpActive() {
		t.Fatal("closing pool destroyed shared connection")
	}
}
func TestDownloadSenderReplacedHolder(t *testing.T) {
	sc := newDownloadSenderCache(func(context.Context, int) (*ExSender, error) { return offlineSender(t), nil })
	defer sc.Close()
	old, err := sc.acquire(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	old.sender.TcpState().SetActive(false)
	fresh, err := sc.acquire(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.release(fresh)
	if old == fresh {
		t.Fatal("dead sender reused")
	}
	sc.release(old)
	if !fresh.sender.IsTcpActive() {
		t.Fatal("releasing stale pool terminated replacement")
	}
}
func TestChunkRequestTimeoutOverride(t *testing.T) {
	j := &downloadJob{opts: &DownloadOptions{RequestTimeout: 8 * time.Second}}
	if got := j.requestTimeout(1048576, 9); got != 8*time.Second {
		t.Fatalf("got %v", got)
	}
	j.opts.RequestTimeout = 0
	if got := j.requestTimeout(1048576, 0); got != 120*time.Second {
		t.Fatalf("lost upstream default: %v", got)
	}
}
func TestWorkerCancellationNotTCPDead(t *testing.T) {
	pool := NewWorkerPool(1)
	defer pool.Close()
	pool.AddWorker(offlineSender(t))
	pool.workers[0].TcpState().SetActive(false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	j := &downloadJob{opts: &DownloadOptions{WorkerPool: pool}}
	_, err := j.fetchPart(ctx, pool, downloadRange{limit: 4096}, 0)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrWorkerTCPDead) {
		t.Fatalf("cancellation misclassified: %v", err)
	}
}
func TestExternalWorkerDeadReturnsImmediately(t *testing.T) {
	pool := NewWorkerPool(1)
	defer pool.Close()
	s := offlineSender(t)
	s.TcpState().SetActive(false)
	pool.AddWorker(s)
	j := &downloadJob{opts: &DownloadOptions{WorkerPool: pool}}
	_, err := j.fetchPart(context.Background(), pool, downloadRange{limit: 4096}, 0)
	if !errors.Is(err, ErrWorkerTCPDead) {
		t.Fatalf("got %v", err)
	}
}
func TestChunkCanceledBeforeSetup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := &Client{}
	if _, _, err := c.DownloadChunkContext(ctx, nil, 0, 4096, 4096); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
func TestChunkRangeBoundaries(t *testing.T) {
	c := &Client{}
	media := &DocumentObj{ID: 1, Size: 7, DcID: 2}
	for _, v := range []struct {
		start, end int
		bad        bool
	}{{0, 0, false}, {7, 7, false}, {-1, 7, true}, {7, 6, true}} {
		b, _, err := c.DownloadChunkContext(context.Background(), media, v.start, v.end, 4096)
		if (err != nil) != v.bad || len(b) != 0 {
			t.Fatalf("%+v: bytes=%d error=%v", v, len(b), err)
		}
	}
	if got := downloadChunkLimit(1047552, 1048600, 1048576); got != 1024 {
		t.Fatalf("crossed 1MB boundary: %d", got)
	}
}

// observedWaitContext marks entry into the blocking select, not merely startup.
type observedWaitContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func (c *observedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}
func TestDownloadSenderCreatorCancelDoesNotCancelFollower(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	sc := newDownloadSenderCache(func(ctx context.Context, dc int) (*ExSender, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return offlineSender(t), nil
	})
	defer sc.Close()
	leader, cancel := context.WithCancel(context.Background())
	defer cancel()
	leadDone := make(chan error, 1)
	go func() { _, err := sc.acquire(leader, 2); leadDone <- err }()
	<-entered
	follower := &observedWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	followDone := make(chan error, 1)
	go func() {
		ref, err := sc.acquire(follower, 2)
		if ref != nil {
			sc.release(ref)
		}
		followDone <- err
	}()
	<-follower.waiting
	cancel()
	if err := <-leadDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case err := <-followDone:
		if err != nil {
			t.Fatalf("active follower inherited creator cancellation: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follower stuck")
	}
	if calls.Load() != 2 {
		t.Fatalf("factory calls: %d", calls.Load())
	}
}
func TestDownloadSenderQueuedWaitCanceled(t *testing.T) {
	entered := make(chan struct{})
	sc := newDownloadSenderCache(func(ctx context.Context, dc int) (*ExSender, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer sc.Close()
	go sc.acquire(context.Background(), 2)
	<-entered
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &observedWaitContext{Context: base, waiting: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := sc.acquire(ctx, 2); done <- err }()
	<-ctx.waiting
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiter did not cancel")
	}
}
func TestDeadWorkerBypassesInternalRetries(t *testing.T) {
	pool := NewWorkerPool(1)
	defer pool.Close()
	sender := offlineSender(t)
	sender.TcpState().SetActive(false)
	pool.AddWorker(sender)
	j := &downloadJob{opts: &DownloadOptions{WorkerPool: pool}}
	calls := 0
	_, err := j.fetchWithRetry(context.Background(), downloadRange{limit: 4096}, func(part downloadRange, attempt int) (downloadResult, error) {
		calls++
		return j.fetchPart(context.Background(), pool, part, attempt)
	})
	if !errors.Is(err, ErrWorkerTCPDead) || calls != 1 {
		t.Fatalf("error=%v attempts=%d", err, calls)
	}
}
