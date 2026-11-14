package telegram

import (
	"bytes"
	"errors"
	"github.com/xAmarnath/gogram/internal/utils"
	"image/png"
	"strings"
	"testing"
)

func TestReplyNilOptions(t *testing.T) {
	for _, opts := range [][]*SendOptions{nil, {nil}, {{Caption: "keep", ReplyID: 5}}} {
		got := replyOptions(42, opts)
		if got == nil || got.ReplyID != 42 {
			t.Fatalf("reply options: %+v", got)
		}
		if len(opts) > 0 && opts[0] != nil {
			if got.Caption != "keep" || opts[0].ReplyID != 5 {
				t.Fatal("lost caller options or mutated shared options")
			}
		}
	}
}
func TestQRScanResultClassification(t *testing.T) {
	c := &Client{Cache: NewCache("", &CacheConfig{Memory: true}), Log: NewDefaultLogger("test")}
	q := &QrToken{client: c}
	if err := q.finishQRLogin(&AuthLoginTokenObj{}); err == nil || !strings.Contains(err.Error(), "scanning again") {
		t.Fatalf("unapproved scan: %v", err)
	}
	if err := q.finishQRLogin(nil); err == nil {
		t.Fatal("unknown result reported successful")
	}
	if err := q.finishQRLogin(&AuthLoginTokenSuccess{Authorization: &AuthAuthorizationObj{User: &UserEmpty{}}}); err == nil {
		t.Fatal("empty user accepted")
	}
	if err := q.finishQRLogin(&AuthLoginTokenSuccess{Authorization: &AuthAuthorizationObj{User: &UserObj{ID: 42, FirstName: "test"}}}); err != nil {
		t.Fatal(err)
	}
	if !q.authorized {
		t.Fatal("successful QR token not marked authorized")
	}
	if u := c.Cache.users[42]; u == nil || u.ID != 42 {
		t.Fatal("authorized user not cached")
	}
}
func TestQRImportPasswordAfterMigration(t *testing.T) {
	called := 0
	want := &AuthLoginTokenSuccess{}
	got, err := importQRTokenWithPassword([]byte{1}, func([]byte) (AuthLoginToken, error) { return nil, errors.New("SESSION_PASSWORD_NEEDED") }, func() (AuthLoginToken, error) { called++; return want, nil })
	if err != nil || got != want || called != 1 {
		t.Fatalf("result=%v error=%v password calls=%d", got, err, called)
	}
	fatal := errors.New("network failed")
	_, err = importQRTokenWithPassword(nil, func([]byte) (AuthLoginToken, error) { return nil, fatal }, func() (AuthLoginToken, error) { t.Fatal("password invoked for network error"); return nil, nil })
	if !errors.Is(err, fatal) {
		t.Fatal(err)
	}
}
func TestQRPNGReadable(t *testing.T) {
	qr, err := utils.NewQRCode("tg://login?token=test")
	if err != nil {
		t.Fatal(err)
	}
	q := &QrToken{}
	q.setQRCode(qr)
	b, err := q.ExportAsPng()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() < 256 || img.Bounds().Dy() < 256 {
		t.Fatal("QR too small to scan")
	}
}

func TestQRRenewAdoptsImmediateSuccess(t *testing.T) {
	q := &QrToken{url: "old", token: []byte{1}, qrBase64: "old image"}
	q.adoptRenewedToken(&QrToken{authorized: true})
	if !q.authorized || q.Url() != "" || len(q.Token()) != 0 || q.qrBase64 != "" {
		t.Fatal("renew retained obsolete QR image")
	}
	if err := q.WaitLogin(); err != nil {
		t.Fatal(err)
	}
}
func TestQRRenewAdoptsNewImage(t *testing.T) {
	q := &QrToken{authorized: true}
	q.adoptRenewedToken(&QrToken{url: "tg://login?token=new", token: []byte{2}, expiresIn: 42, qrBase64: "new image", qrSmall: "new terminal"})
	if q.authorized || q.Url() != "tg://login?token=new" || q.qrBase64 != "new image" || q.qrSmall != "new terminal" || q.expiresIn != 42 {
		t.Fatal("renew lost QR state")
	}
}
