package push

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type serviceAccount struct {
	ProjectID  string `json:"project_id"`
	KeyID      string `json:"key_id"`
	SubAccount string `json:"sub_account"`
	PrivateKey string `json:"private_key"`
}

type Huawei struct {
	account  serviceAccount
	key      *rsa.PrivateKey
	client   *http.Client
	endpoint string
	mu       sync.Mutex
	jwt      string
	expires  time.Time
}

// Error intentionally excludes the response message, which can contain device tokens.
type Error struct {
	Status       int
	Code         string
	InvalidToken bool
}

func (e *Error) Error() string { return fmt.Sprintf("Push Kit HTTP %d, code %s", e.Status, e.Code) }
func (e *Error) Temporary() bool {
	return e.Status == 429 || e.Status >= 500 || e.Code == "81000001" || e.Code == "80200005"
}

func Load(path string) (*Huawei, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Push Kit service account: %w", err)
	}
	var a serviceAccount
	if json.Unmarshal(data, &a) != nil {
		return nil, fmt.Errorf("invalid Push Kit service account JSON")
	}
	if !regexp.MustCompile(`^[0-9]+$`).MatchString(a.ProjectID) || a.KeyID == "" || a.SubAccount == "" {
		return nil, fmt.Errorf("Push Kit requires project_id, key_id and sub_account")
	}
	block, _ := pem.Decode([]byte(a.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("invalid Push Kit private key PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid Push Kit PKCS8 private key")
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() < 2048 {
		return nil, fmt.Errorf("Push Kit requires an RSA private key of at least 2048 bits")
	}
	if err = key.Validate(); err != nil {
		return nil, fmt.Errorf("invalid Push Kit RSA private key")
	}
	a.PrivateKey = ""
	return &Huawei{account: a, key: key, client: &http.Client{Timeout: 10 * time.Second}, endpoint: "https://push-api.cloud.huawei.com/v3/" + a.ProjectID + "/messages:send"}, nil
}

func (h *Huawei) authorization() (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if h.jwt != "" && now.Add(time.Minute).Before(h.expires) {
		return h.jwt, nil
	}
	header, _ := json.Marshal(map[string]string{"alg": "PS256", "typ": "JWT", "kid": h.account.KeyID})
	payload, _ := json.Marshal(map[string]any{"iss": h.account.SubAccount, "aud": "https://oauth-login.cloud.huawei.com/oauth2/v3/token", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPSS(rand.Reader, h.key, crypto.SHA256, digest[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		return "", err
	}
	h.jwt = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	h.expires = now.Add(time.Hour)
	return h.jwt, nil
}

func (h *Huawei) Send(ctx context.Context, token, entryID string) error {
	jwt, err := h.authorization()
	if err != nil {
		return err
	}
	body := "其他设备向在线剪贴板上传了新内容，点击查看。"
	id, err := strconv.ParseUint(entryID, 10, 64)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]any{
		"payload": map[string]any{"notification": map[string]any{
			"category": "WORK", "title": "Paste Anywhere", "body": body,
			"clickAction": map[string]int{"actionType": 0}, "foregroundShow": true,
			"notifyId": id % 2147483648,
		}},
		"target":      map[string]any{"token": []string{token}},
		"pushOptions": map[string]int{"ttl": 300},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("push-type", "0")
	response, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("Push Kit transport failed")
	}
	defer response.Body.Close()
	var result struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&result)
	if response.StatusCode == 200 && decodeErr == nil && result.Code == "80000000" {
		return nil
	}
	failure := &Error{Status: response.StatusCode, Code: result.Code}
	if result.Code == "80200005" {
		h.mu.Lock()
		h.jwt = ""
		h.mu.Unlock()
	}
	// 80300007 can also mean missing rights or a wrong project; only remove proven malformed tokens.
	if result.Code == "80300007" || result.Code == "80100000" {
		var details struct {
			IllegalTokens map[string][]string `json:"illegalTokens"`
		}
		if json.Unmarshal([]byte(result.Msg), &details) == nil {
			for _, invalid := range details.IllegalTokens["tokenFormatError"] {
				if invalid == token {
					failure.InvalidToken = true
				}
			}
		}
	}
	return failure
}
