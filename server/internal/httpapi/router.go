package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"paste-anywhere/server/internal/storage"
)

type Event struct {
	Type           string `json:"type"`
	ItemID         string `json:"itemId,omitempty"`
	SourceDeviceID string `json:"sourceDeviceId,omitempty"`
}
type subscription struct {
	device   storage.Device
	events   chan Event
	overflow chan struct{}
}
type API struct {
	store     *storage.Store
	maxText   int
	maxImage  int
	imageWork chan struct{}
	mu        sync.Mutex // serializes commits, publish, and subscription start; no offline replay
	subs      map[*subscription]bool
}

func New(store *storage.Store, maxText int, maxImage int) *API {
	return &API{store: store, maxText: maxText, maxImage: maxImage, imageWork: make(chan struct{}, 1), subs: map[*subscription]bool{}}
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /v1/connect", a.connect)
	mux.HandleFunc("GET /v1/users", a.auth(a.users))
	mux.HandleFunc("POST /v1/entries", a.auth(a.upload))
	mux.HandleFunc("POST /v1/images", a.auth(a.uploadImage))
	mux.HandleFunc("GET /v1/entries/{id}/image", a.auth(a.downloadImage))
	mux.HandleFunc("GET /v1/clipboard", a.auth(a.latest))
	mux.HandleFunc("GET /v1/history", a.auth(a.history))
	mux.HandleFunc("GET /v1/entries/{id}", a.auth(a.entry))
	mux.HandleFunc("POST /v1/entries/{id}/send", a.auth(a.send))
	mux.HandleFunc("GET /v1/events", a.auth(a.events))
	return mux
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	write(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, v any, max int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, max)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		fail(w, 400, "invalid or oversized JSON body")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		fail(w, 400, "expected one JSON value")
		return false
	}
	return true
}
func (a *API) auth(next func(http.ResponseWriter, *http.Request, storage.Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" || token == r.Header.Get("Authorization") {
			fail(w, 401, "device authentication required")
			return
		}
		d, e := a.store.Authenticate(token)
		if e != nil {
			fail(w, 401, "invalid device credential")
			return
		}
		next(w, r, d)
	}
}
func (a *API) connect(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Invite     string `json:"invite"`
		DeviceName string `json:"deviceName"`
	}
	if !decode(w, r, &input, 4096) {
		return
	}
	input.DeviceName = strings.TrimSpace(input.DeviceName)
	if len(input.Invite) != 64 || len(input.DeviceName) == 0 || len(input.DeviceName) > 128 {
		fail(w, 400, "invalid invite or device name")
		return
	}
	d, u, token, e := a.store.Connect(input.Invite, input.DeviceName)
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 401, "invalid invite")
		return
	}
	if e != nil {
		a.internal(w, e)
		return
	}
	write(w, 201, struct {
		Device storage.Device `json:"device"`
		User   storage.User   `json:"user"`
		Token  string         `json:"token"`
	}{d, u, token})
}
func (a *API) internal(w http.ResponseWriter, e error) {
	log.Printf("API storage error: %v", e)
	fail(w, 500, "storage error")
}
func (a *API) users(w http.ResponseWriter, r *http.Request, d storage.Device) {
	u, e := a.store.Users()
	if e != nil {
		a.internal(w, e)
		return
	}
	write(w, 200, u)
}
func (a *API) latest(w http.ResponseWriter, r *http.Request, d storage.Device) {
	e, err := a.store.Latest(d.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		write(w, 200, nil)
		return
	}
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, e)
}
func (a *API) entry(w http.ResponseWriter, r *http.Request, d storage.Device) {
	e, err := a.store.Entry(d.UserID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "entry not found")
		} else {
			a.internal(w, err)
		}
		return
	}
	write(w, 200, e)
}
func (a *API) history(w http.ResponseWriter, r *http.Request, d storage.Device) {
	e, err := a.store.History(d.UserID)
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, e)
}
func (a *API) upload(w http.ResponseWriter, r *http.Request, d storage.Device) {
	var input struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &input, int64(a.maxText)*6+1024) {
		return
	}
	if len(input.Text) == 0 || len(input.Text) > a.maxText {
		fail(w, 400, "empty text or text exceeds configured limit")
		return
	}
	a.create(w, d, func() (storage.Entry, error) { return a.store.CreateEntry(d, d.UserID, input.Text) })
}
func (a *API) send(w http.ResponseWriter, r *http.Request, d storage.Device) {
	var input struct {
		UserID string `json:"userId"`
	}
	if !decode(w, r, &input, 1024) {
		return
	}
	a.create(w, d, func() (storage.Entry, error) { return a.store.Forward(d, r.PathValue("id"), input.UserID) })
}
func (a *API) create(w http.ResponseWriter, d storage.Device, commit func() (storage.Entry, error)) {
	a.mu.Lock()
	e, err := commit()
	if err == nil {
		for sub := range a.subs {
			if sub.device.UserID == e.UserID && sub.device.ID != d.ID {
				select {
				case sub.events <- Event{"clipboard.updated", e.ID, d.ID}:
				default:
					close(sub.overflow)
					delete(a.subs, sub)
				}
			}
		}
	}
	a.mu.Unlock()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "device or recipient no longer exists")
		} else {
			a.internal(w, err)
		}
		return
	}
	write(w, 201, e)
}

func (a *API) uploadImage(w http.ResponseWriter, r *http.Request, d storage.Device) {
	select {
	case a.imageWork <- struct{}{}:
		defer func() { <-a.imageWork }()
	case <-r.Context().Done():
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, int64(a.maxImage))
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, 413, "image exceeds configured byte limit")
		} else {
			fail(w, 400, "could not read image")
		}
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		fail(w, 415, "expected a PNG or JPEG image")
		return
	}
	if config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		fail(w, 400, "image exceeds 40 megapixels")
		return
	}
	if _, _, err = image.Decode(bytes.NewReader(data)); err != nil {
		fail(w, 400, "invalid or truncated image")
		return
	}
	mime, name := "image/png", "image.png"
	if format == "jpeg" {
		mime, name = "image/jpeg", "image.jpg"
	}
	attachment := storage.Attachment{Name: name, MIME: mime, Width: config.Width, Height: config.Height}
	a.create(w, d, func() (storage.Entry, error) { return a.store.CreateImage(d, data, attachment) })
}

func (a *API) downloadImage(w http.ResponseWriter, r *http.Request, d storage.Device) {
	f, attachment, err := a.store.OpenImage(d.UserID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "image not found")
		} else {
			a.internal(w, err)
		}
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", attachment.MIME)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `inline; filename="`+attachment.Name+`"`)
	http.ServeContent(w, r, attachment.Name, time.Time{}, f)
}
func (a *API) events(w http.ResponseWriter, r *http.Request, d storage.Device) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("WebSocket handshake rejected: %v", err)
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	sub := &subscription{d, make(chan Event, 32), make(chan struct{})}
	a.mu.Lock()
	a.subs[sub] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.subs, sub); a.mu.Unlock() }()
	if !sendEvent(ctx, c, Event{Type: "ready"}) {
		return
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.overflow:
			_ = c.Close(websocket.StatusPolicyViolation, "slow consumer; reconnect for future updates")
			return
		case e := <-sub.events:
			if !sendEvent(ctx, c, e) {
				return
			}
		case <-ticker.C:
			if _, err := a.store.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil {
				_ = c.Close(websocket.StatusPolicyViolation, "credential revoked")
				return
			}
			ping, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Ping(ping)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func sendEvent(ctx context.Context, c *websocket.Conn, e Event) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, e) == nil
}
