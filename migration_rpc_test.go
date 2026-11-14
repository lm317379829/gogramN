package gogram

import (
	"context"
	"errors"
	"github.com/xAmarnath/gogram/internal/encoding/tl"
	"github.com/xAmarnath/gogram/internal/mtproto/messages"
	"github.com/xAmarnath/gogram/internal/mtproto/objects"
	"github.com/xAmarnath/gogram/internal/utils"
	"net"
	"testing"
)

type offlineRPCTransport struct {
	client   *MTProto
	response func(messages.Common) tl.Object
	writes   int
}

func (tr *offlineRPCTransport) Close() error                      { return nil }
func (tr *offlineRPCTransport) ReadMsg() (messages.Common, error) { return nil, net.ErrClosed }
func (tr *offlineRPCTransport) WriteMsg(msg messages.Common, seq int32) error {
	tr.writes++
	if tr.response != nil {
		if obj := tr.response(msg); obj != nil {
			return tr.client.writeRPCResponse(msg.GetMsgID(), obj)
		}
	}
	return nil
}
func offlineRPC(t *testing.T, cfg Config) (*MTProto, *offlineRPCTransport) {
	t.Helper()
	cfg.MemorySession = true
	m, err := NewMTProto(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tr := &offlineRPCTransport{client: m}
	m.transport = tr
	m.encrypted.Store(true)
	m.tcpState.SetActive(true)
	t.Cleanup(func() { m.Terminate() })
	return m, tr
}
func TestMakeRequestFloodWaitCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m, tr := offlineRPC(t, Config{FloodHandlerContext: func(c context.Context, err error) bool { cancel(); <-c.Done(); return false }})
	tr.response = func(messages.Common) tl.Object {
		return &objects.RpcError{ErrorCode: 420, ErrorMessage: "FLOOD_WAIT_60"}
	}
	_, err := m.MakeRequest(ctx, &utils.PingParams{PingID: 42})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost RPC cancellation: %v", err)
	}
	if tr.writes != 1 {
		t.Fatalf("retried canceled RPC %d times", tr.writes)
	}
	if !m.IsTcpActive() {
		t.Fatal("cancel disconnected shared connection")
	}
}
