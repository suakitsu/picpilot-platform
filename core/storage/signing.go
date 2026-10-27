package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"time"
)

func SignAsset(key []byte, id, owner string, expires int64) string {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte("picpilot-asset-v1\n" + id + "\n" + owner + "\n" + strconv.FormatInt(expires, 10)))
	return hex.EncodeToString(m.Sum(nil))
}

func VerifyAsset(key []byte, id, owner, signature string, expires int64, now time.Time) bool {
	if len(key) != 32 || expires <= now.Unix() || expires > now.Add(time.Hour+time.Minute).Unix() {
		return false
	}
	actual, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	expected, _ := hex.DecodeString(SignAsset(key, id, owner, expires))
	return hmac.Equal(actual, expected)
}

func AssetURL(base string, key []byte, id, owner string, now time.Time) string {
	expires := now.Add(time.Hour).Unix()
	q := url.Values{"expiresAt": {strconv.FormatInt(expires, 10)}, "signature": {SignAsset(key, id, owner, expires)}}
	return base + "/api/files/assets/" + url.PathEscape(id) + "?" + q.Encode()
}
