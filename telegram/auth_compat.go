package telegram

import (
	"fmt"
	"github.com/xAmarnath/gogram/internal/utils"
)

func replyOptions(id int32, opts []*SendOptions) *SendOptions {
	result := &SendOptions{}
	if len(opts) > 0 && opts[0] != nil {
		*result = *opts[0]
	}
	result.ReplyID = id
	return result
}
func (q *QrToken) setQRCode(code *utils.QRCode) {
	q.qrBase64 = code.Base64PNG(256)
	q.qrSmall = code.ToSmallString(false)
}
func importQRTokenWithPassword(token []byte, importLogin func([]byte) (AuthLoginToken, error), password func() (AuthLoginToken, error)) (AuthLoginToken, error) {
	resp, err := importLogin(token)
	if err != nil && MatchError(err, "SESSION_PASSWORD_NEEDED") {
		return password()
	}
	return resp, err
}
func (q *QrToken) finishQRLogin(resp AuthLoginToken) error {
	switch value := resp.(type) {
	case *AuthLoginTokenSuccess:
		if value == nil {
			return fmt.Errorf("nil QR login success")
		}
		auth, ok := value.Authorization.(*AuthAuthorizationObj)
		if !ok || auth == nil {
			return fmt.Errorf("unexpected QR authorization: %T", value.Authorization)
		}
		user, ok := auth.User.(*UserObj)
		if !ok || user == nil || user.ID == 0 {
			return fmt.Errorf("invalid user after QR login: %T", auth.User)
		}
		q.client.Cache.UpdateUser(user)
		q.authorized = true
		return nil
	case *AuthLoginTokenObj:
		return fmt.Errorf("scan received but login not approved, please try scanning again")
	default:
		return fmt.Errorf("unexpected response type after scan: %T", resp)
	}
}

// adoptRenewedToken also handles QRLogin returning an already authorized token.
func (q *QrToken) adoptRenewedToken(next *QrToken) {
	q.authorized = next.authorized
	q.token, q.url, q.expiresIn = next.token, next.url, next.expiresIn
	q.timeout, q.maxRetries = next.timeout, next.maxRetries
	q.passwordCallback, q.onWrongPassword = next.passwordCallback, next.onWrongPassword
	q.qrBase64, q.qrSmall = next.qrBase64, next.qrSmall
}
