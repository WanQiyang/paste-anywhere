package httpapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"paste-anywhere/server/internal/storage"
)

func TestImageUploadDownloadForwardAndIsolation(t *testing.T) {
	s, err := storage.Open(filepath.Join(t.TempDir(), "images.db"), 20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	user, invite, _ := s.CreateUser("source")
	other, invite2, _ := s.CreateUser("recipient")
	_, _, token, _ := s.Connect(invite, "upload")
	_, _, receiveToken, _ := s.Connect(invite, "receive")
	_, _, token2, _ := s.Connect(invite2, "other")
	server := httptest.NewServer(New(s, 1024, 4096).Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/events", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + receiveToken}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	var event Event
	if err = wsjson.Read(ctx, ws, &event); err != nil || event.Type != "ready" {
		t.Fatal("not ready", err)
	}
	request := func(method, path, credential string, body []byte, status int) []byte {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+credential)
		r.Header.Set("Content-Type", "application/json")
		if method == "POST" && path == "/v1/entries" {
			contentType := "image/png"
			if len(body) >= 2 && body[0] == 0xFF && body[1] == 0xD8 {
				contentType = "image/jpeg"
			}
			r.Header.Set("Content-Type", contentType)
		}
		response, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		data, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		if response.StatusCode != status {
			t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, data)
		}
		return data
	}
	pixels := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	pixels.SetNRGBA(1, 1, color.NRGBA{R: 240, G: 100, B: 60, A: 128})
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	var entry storage.Entry
	if err = json.Unmarshal(request("POST", "/v1/entries", token, encoded.Bytes(), 201), &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Kind != "image" || entry.UserID != user.ID || entry.Attachment == nil || entry.Attachment.MIME != "image/png" || entry.Attachment.Width != 3 || entry.Attachment.Height != 2 {
		t.Fatalf("invalid metadata: %+v", entry)
	}
	if err = wsjson.Read(ctx, ws, &event); err != nil || event.ItemID != entry.ID {
		t.Fatal("image notification missing", err)
	}
	if data := request("GET", "/v1/entries/"+entry.ID+"/content", receiveToken, nil, 200); !bytes.Equal(data, encoded.Bytes()) {
		t.Fatal("image bytes changed")
	}
	request("GET", "/v1/entries/"+entry.ID+"/content", token2, nil, 404)
	request("GET", "/v1/entries/"+entry.ID+"/content", "", nil, 401)
	var forwarded storage.Entry
	_ = json.Unmarshal(request("POST", "/v1/transfers", token, []byte(fmt.Sprintf(`{"entryId":%q,"userId":%q}`, entry.ID, other.ID)), 201), &forwarded)
	if forwarded.ID == entry.ID || forwarded.Attachment == nil || forwarded.Attachment.ID != entry.Attachment.ID {
		t.Fatal("forward did not reuse the image file with a new entry")
	}
	if data := request("GET", "/v1/entries/"+forwarded.ID+"/content", token2, nil, 200); !bytes.Equal(data, encoded.Bytes()) {
		t.Fatal("forwarded bytes differ")
	}
	request("POST", "/v1/entries", token, []byte("not an image"), 415)
	request("POST", "/v1/entries", token, encoded.Bytes()[:len(encoded.Bytes())/2], 400)
	request("POST", "/v1/entries", token, make([]byte, 4097), 413)
	largeDimensions := append([]byte(nil), encoded.Bytes()...)
	binary.BigEndian.PutUint32(largeDimensions[16:20], 40_000_001)
	binary.BigEndian.PutUint32(largeDimensions[20:24], 1)
	binary.BigEndian.PutUint32(largeDimensions[29:33], crc32.ChecksumIEEE(largeDimensions[12:29]))
	request("POST", "/v1/entries", token, largeDimensions, 400)
	encoded.Reset()
	if err = jpeg.Encode(&encoded, pixels, nil); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(request("POST", "/v1/entries", token, encoded.Bytes(), 201), &entry)
	if entry.Attachment.MIME != "image/jpeg" {
		t.Fatal("JPEG type not detected")
	}
}
