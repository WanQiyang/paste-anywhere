package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"paste-anywhere/server/internal/push"
	"paste-anywhere/server/internal/storage"
)

type PushSender interface {
	Send(context.Context, string, string) error
}
type pushJob struct {
	userID, entryID string
	targets         []storage.PushTarget
	created         time.Time
}

// StartPush is called before serving HTTP. The returned function cancels and joins the worker.
func (a *API) StartPush(parent context.Context, sender PushSender) func() {
	ctx, cancel := context.WithCancel(parent)
	a.pushQueue = make(chan pushJob, 128)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case job := <-a.pushQueue:
				a.deliverPush(ctx, sender, job)
			}
		}
	}()
	return func() { cancel(); <-done }
}

func (a *API) setPushToken(w http.ResponseWriter, r *http.Request, d storage.Device) {
	var input struct {
		Token *string `json:"token"`
	}
	if !decode(w, r, &input, 8192) {
		return
	}
	if input.Token == nil || len(*input.Token) > 4096 || strings.IndexFunc(*input.Token, func(c rune) bool { return c <= 32 || c >= 127 }) >= 0 {
		fail(w, 400, "invalid Push Token")
		return
	}
	if err := a.store.SetPushToken(d, *input.Token); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fail(w, 401, "invalid device credential")
		} else {
			a.internal(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Called under the commit mutex, so recipients are captured at publication time.
func (a *API) queuePush(e storage.Entry) {
	if a.pushQueue == nil {
		return
	}
	targets, err := a.store.PushTargets(e.UserID, e.SourceDeviceID)
	if err != nil {
		log.Printf("push targets: %v", err)
		return
	}
	if len(targets) == 0 {
		return
	}
	select {
	case a.pushQueue <- pushJob{e.UserID, e.ID, targets, time.Now()}:
	default:
		log.Printf("push queue full: entry %s", e.ID)
	}
}

func (a *API) deliverPush(ctx context.Context, sender PushSender, job pushJob) {
	for _, target := range job.targets {
		for attempt := 0; attempt < 3; attempt++ {
			if ctx.Err() != nil || time.Since(job.created) >= 5*time.Minute {
				return
			}
			pending, err := a.store.PushPending(target, job.userID, job.entryID)
			if err != nil {
				log.Printf("push lookup: %v", err)
				break
			}
			if !pending {
				break
			}
			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = sender.Send(sendCtx, target.Token, job.entryID)
			cancel()
			if err == nil {
				break
			}
			log.Printf("push entry %s: %v", job.entryID, err)
			var failure *push.Error
			if !errors.As(err, &failure) {
				break
			} // Ambiguous transport failures are not replayed.
			if failure.InvalidToken {
				if err = a.store.RemovePushToken(target); err != nil {
					log.Printf("remove push token: %v", err)
				}
				break
			}
			if !failure.Temporary() || attempt == 2 {
				break
			}
			timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}
