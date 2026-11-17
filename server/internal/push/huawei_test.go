package push

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHuaweiJWTAndNotification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	data, _ := json.Marshal(serviceAccount{"12345", "test-key", "test-account", string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))})
	path := filepath.Join(t.TempDir(), "key.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	h, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if h.endpoint != "https://push-api.cloud.huawei.com/v3/12345/messages:send" {
		t.Fatal(h.endpoint)
	}
	code, message := "80000000", "Success"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("push-type") != "0" {
			t.Error("incorrect method or push-type")
		}
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
		if len(parts) != 3 {
			t.Error("invalid JWT")
			return
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
		if err := rsa.VerifyPSS(&key.PublicKey, crypto.SHA256, digest[:], signature, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
			t.Error(err)
		}
		headerJSON, _ := base64.RawURLEncoding.DecodeString(parts[0])
		var header map[string]string
		json.Unmarshal(headerJSON, &header)
		if header["alg"] != "PS256" || header["kid"] != "test-key" {
			t.Error(header)
		}
		claimsJSON, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Iss string
			Aud string
			Iat int64
			Exp int64
		}
		json.Unmarshal(claimsJSON, &claims)
		if claims.Iss != "test-account" || claims.Aud != "https://oauth-login.cloud.huawei.com/oauth2/v3/token" || claims.Exp-claims.Iat != 3600 || claims.Exp <= time.Now().Unix() {
			t.Error(claims)
		}
		var body struct {
			Payload struct {
				Notification struct {
					Category    string
					Body        string
					NotifyID    int64 `json:"notifyId"`
					ClickAction struct {
						ActionType int `json:"actionType"`
					} `json:"clickAction"`
				}
			}
			Target      struct{ Token []string }
			PushOptions struct{ TTL int }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Payload.Notification.Category != "WORK" || body.Payload.Notification.NotifyID != 42 || body.Payload.Notification.Body != "其他设备向在线剪贴板上传了新内容，点击查看。" || body.PushOptions.TTL != 300 || len(body.Target.Token) != 1 || body.Target.Token[0] != "test-token" {
			t.Errorf("wrong payload: %+v", body)
		}
		json.NewEncoder(w).Encode(map[string]string{"code": code, "msg": message})
	}))
	defer server.Close()
	h.endpoint = server.URL
	if err = h.Send(context.Background(), "test-token", "42"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		code, msg          string
		invalid, temporary bool
	}{
		{"80300007", `{"illegalTokens":{"noRight":["test-token"]}}`, false, false},
		{"80300007", `{"illegalTokens":{"tokenFormatError":["test-token"]}}`, true, false},
		{"81000001", "error", false, true},
		{"80200005", "expired", false, true},
	} {
		code, message = test.code, test.msg
		err = h.Send(context.Background(), "test-token", "42")
		var failure *Error
		if !errors.As(err, &failure) || failure.InvalidToken != test.invalid || failure.Temporary() != test.temporary {
			t.Fatalf("unexpected error %v", err)
		}
		if strings.Contains(err.Error(), "test-token") {
			t.Fatal("token leaked in error")
		}
	}
	if h.jwt != "" {
		t.Fatal("expired JWT was not invalidated")
	}
}
