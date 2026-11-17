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
	"mime"
	"net"
	"net/http"
	"strconv"
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
	TrustProxy bool
	store      *storage.Store
	maxText    int
	maxImage   int
	imageWork  chan struct{}
	mu         sync.Mutex // serializes commits, publish, and subscription start; no offline replay
	subs       map[*subscription]bool
	pushQueue  chan pushJob
}

func New(store *storage.Store, maxText int, maxImage int) *API {
	return &API{store: store, maxText: maxText, maxImage: maxImage, imageWork: make(chan struct{}, 1), subs: map[*subscription]bool{}}
}
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /v1/devices", a.createDevice)
	mux.HandleFunc("PUT /v1/push-token", a.auth(a.setPushToken))
	mux.HandleFunc("DELETE /v1/devices/{name}", a.auth(a.deleteDevice))
	mux.HandleFunc("GET /v1/users", a.auth(a.users))
	mux.HandleFunc("POST /v1/entries", a.auth(a.upload))
	mux.HandleFunc("GET /v1/entries/{id}/content", a.auth(a.content))
	mux.HandleFunc("DELETE /v1/entries", a.auth(a.clearClipboard))
	mux.HandleFunc("GET /v1/entries", a.auth(a.listEntries))
	mux.HandleFunc("GET /v1/entries/{id}", a.auth(a.entry))
	mux.HandleFunc("POST /v1/transfers", a.auth(a.createTransfer))
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
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		fail(w, 415, "Content-Type must be application/json")
		return false
	}
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
			if errors.Is(e, sql.ErrNoRows) {
				fail(w, 401, "invalid device credential")
			} else {
				a.internal(w, e)
			}
			return
		}
		if err := a.recordActivity(r, d.ID); err != nil {
			a.internal(w, err)
			return
		}
		next(w, r, d)
	}
}
func (a *API) createDevice(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Invite     string `json:"invite"`
		DeviceName string `json:"deviceName"`
	}
	if !decode(w, r, &input, 4096) {
		return
	}
	input.DeviceName = strings.TrimSpace(input.DeviceName)
	if len(input.Invite) != 8 || !storage.ValidName(input.DeviceName) {
		fail(w, 400, "invalid invite or device name")
		return
	}
	d, u, token, e := a.store.Connect(input.Invite, input.DeviceName)
	if errors.Is(e, storage.ErrNameExists) {
		fail(w, http.StatusConflict, "device name exists")
		return
	}
	if errors.Is(e, storage.ErrInvalidName) {
		fail(w, http.StatusBadRequest, "invalid user or device name")
		return
	}
	if errors.Is(e, sql.ErrNoRows) {
		fail(w, 401, "invalid invite")
		return
	}
	if e != nil {
		a.internal(w, e)
		return
	}
	// Pairing has committed; metadata failure must not strand the new credential.
	if err := a.recordActivity(r, d.ID); err != nil {
		log.Printf("device metadata: %v", err)
	}
	w.Header().Set("Location", "/v1/devices/"+u.Name+"-"+d.Name)
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
	a.mu.Lock()
	online := make(map[string]map[string]bool)
	for sub := range a.subs {
		if online[sub.device.UserID] == nil {
			online[sub.device.UserID] = make(map[string]bool)
		}
		online[sub.device.UserID][sub.device.ID] = true
	}
	a.mu.Unlock()
	result := make([]PresenceUser, 0, len(u))
	for _, user := range u {
		count := len(online[user.ID])
		result = append(result, PresenceUser{user.ID, user.Name, count})
	}
	write(w, 200, result)
}

type PresenceUser struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OnlineDevices int    `json:"onlineDevices"`
}

func (a *API) clearClipboard(w http.ResponseWriter, r *http.Request, d storage.Device) {
	a.mu.Lock()
	through, err := a.store.ClearClipboard(d)
	event := Event{Type: "clipboard.cleared", ItemID: through, SourceDeviceID: d.ID}
	if err == nil {
		a.publish(d.UserID, "", event)
	}
	a.mu.Unlock()
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "invalid device credential")
		} else {
			a.internal(w, err)
		}
		return
	}
	write(w, 200, event)
}

func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request, d storage.Device) {
	name := r.PathValue("name")
	userName, deviceName, valid := strings.Cut(name, "-")
	if !valid || !storage.ValidName(userName) || !storage.ValidName(deviceName) {
		fail(w, 400, "invalid device name")
		return
	}
	identity, err := a.store.DeviceIdentity(d.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "invalid device credential")
		} else {
			a.internal(w, err)
		}
		return
	}
	if identity != name {
		fail(w, 403, "only the authenticated device can be deleted")
		return
	}
	a.mu.Lock()
	err = a.store.RevokeDevice(d.ID)
	if err == nil {
		for sub := range a.subs {
			if sub.device.ID == d.ID {
				close(sub.overflow)
				delete(a.subs, sub)
			}
		}
	}
	a.mu.Unlock()
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		a.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
func (a *API) listEntries(w http.ResponseWriter, r *http.Request, d storage.Device) {
	limit := a.store.HistoryLimit
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 {
			fail(w, 400, "invalid entry limit")
			return
		}
		limit = parsed
	}
	e, err := a.store.Recent(d.UserID, limit)
	if err != nil {
		a.internal(w, err)
		return
	}
	write(w, 200, e)
}
func (a *API) upload(w http.ResponseWriter, r *http.Request, d storage.Device) {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		fail(w, 415, "Content-Type must be application/json, image/png or image/jpeg")
		return
	}
	switch contentType {
	case "image/png", "image/jpeg":
		a.uploadImage(w, r, d, contentType)
		return
	case "application/json":
	default:
		fail(w, 415, "Content-Type must be application/json, image/png or image/jpeg")
		return
	}
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
func (a *API) createTransfer(w http.ResponseWriter, r *http.Request, d storage.Device) {
	var input struct {
		EntryID string `json:"entryId"`
		UserID  string `json:"userId"`
	}
	if !decode(w, r, &input, 1024) {
		return
	}
	if input.EntryID == "" || input.UserID == "" {
		fail(w, 400, "entryId and userId are required")
		return
	}
	a.create(w, d, func() (storage.Entry, error) { return a.store.Forward(d, input.EntryID, input.UserID) })
}
func (a *API) create(w http.ResponseWriter, d storage.Device, commit func() (storage.Entry, error)) {
	a.mu.Lock()
	e, err := commit()
	if err == nil {
		a.publish(e.UserID, d.ID, Event{"clipboard.updated", e.ID, d.ID})
		a.queuePush(e)
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
	w.Header().Set("Location", "/v1/entries/"+e.ID)
	write(w, 201, e)
}

// Caller holds mu so commits, delivery and subscription registration stay ordered.
func (a *API) publish(userID, skipDeviceID string, event Event) {
	for sub := range a.subs {
		if sub.device.UserID != userID || sub.device.ID == skipDeviceID {
			continue
		}
		select {
		case sub.events <- event:
		default:
			close(sub.overflow)
			delete(a.subs, sub)
		}
	}
}

func (a *API) uploadImage(w http.ResponseWriter, r *http.Request, d storage.Device, contentType string) {
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
	if contentType != "image/"+format {
		fail(w, 415, "image format does not match Content-Type")
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

func (a *API) content(w http.ResponseWriter, r *http.Request, d storage.Device) {
	entry, err := a.store.Entry(d.UserID, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 404, "entry not found")
		} else {
			a.internal(w, err)
		}
		return
	}
	if entry.Kind == "text" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "clipboard.txt", time.UnixMilli(entry.CreatedAt), strings.NewReader(entry.Text))
		return
	}
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
	if _, err := a.store.Authenticate(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")); err != nil {
		a.mu.Unlock()
		_ = c.Close(websocket.StatusPolicyViolation, "credential revoked")
		return
	}
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
			_ = c.Close(websocket.StatusPolicyViolation, "subscription ended")
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
			if err := a.store.TouchDevice(d.ID); err != nil {
				log.Printf("device activity: %v", err)
			}
		}
	}
}

func (a *API) recordActivity(r *http.Request, id string) error {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if a.TrustProxy {
		if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
			host = ip.String()
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		host = ip.String()
	} else {
		host = ""
	}
	platform := r.Header.Get("X-Paste-Platform")
	if platform != "Windows" && platform != "HarmonyOS" {
		platform = ""
	}
	version := r.Header.Get("X-Paste-Version")
	if len(version) > 64 {
		version = ""
	}
	for _, c := range version {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '-' || c == '+') {
			version = ""
			break
		}
	}
	return a.store.RecordActivity(id, platform, version, host)
}

func (a *API) SavePresence() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.subs))
	for sub := range a.subs {
		ids = append(ids, sub.device.ID)
	}
	return a.store.SavePresence(ids)
}

func sendEvent(ctx context.Context, c *websocket.Conn, e Event) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return wsjson.Write(ctx, c, e) == nil
}
