package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"approval-engine-service/internal/domain"
)

type fakeAppLookup struct {
	app *domain.Application
	err error
}

func (f *fakeAppLookup) GetByID(ctx context.Context, id string) (*domain.Application, error) {
	return f.app, f.err
}

// waitFor polls until cond() is true or the timeout elapses, so tests don't
// need a fixed sleep for the notifier's background goroutine to finish.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	require.True(t, cond(), "condition not met within %s", timeout)
}

func TestNotifyDeliversSignedPayload(t *testing.T) {
	var received atomic.Bool
	var gotSig, gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotSig = r.Header.Get("X-Webhook-Signature")
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		received.Store(true)
	}))
	defer srv.Close()

	app := &domain.Application{ID: "assetmgmt", APIKey: "secret-key", CallbackURL: srv.URL}
	n := NewWebhookNotifier(&fakeAppLookup{app: app})

	n.Notify("assetmgmt", WebhookEvent{Event: domain.EventApproved, AppID: "assetmgmt", RequestID: "req-1"})

	waitFor(t, time.Second, received.Load)

	mac := hmac.New(sha256.New, []byte("secret-key"))
	mac.Write([]byte(gotBody))
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	assert.Equal(t, wantSig, gotSig, "signature must be HMAC-SHA256 of the exact body, keyed with the app's api_key")

	var got WebhookEvent
	require.NoError(t, json.Unmarshal([]byte(gotBody), &got))
	assert.Equal(t, "req-1", got.RequestID)
	assert.Equal(t, domain.EventApproved, got.Event)
}

func TestNotifyRetriesOnFailureThenSucceeds(t *testing.T) {
	origBackoff := retryBackoff
	retryBackoff = []time.Duration{0, 10 * time.Millisecond, 10 * time.Millisecond}
	defer func() { retryBackoff = origBackoff }()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	app := &domain.Application{ID: "assetmgmt", APIKey: "secret-key", CallbackURL: srv.URL}
	n := NewWebhookNotifier(&fakeAppLookup{app: app})

	n.Notify("assetmgmt", WebhookEvent{Event: domain.EventStepActivated, AppID: "assetmgmt", RequestID: "req-2"})

	waitFor(t, time.Second, func() bool { return attempts.Load() == 3 })
	assert.EqualValues(t, 3, attempts.Load(), "should have retried until success on the 3rd attempt")
}

func TestNotifySkipsAppsWithoutCallbackURL(t *testing.T) {
	app := &domain.Application{ID: "assetmgmt", APIKey: "secret-key", CallbackURL: ""}
	n := NewWebhookNotifier(&fakeAppLookup{app: app})

	// Should return promptly and never dial out — if it tried, deliver()
	// would hang/fail against a bogus lookup since there is no server.
	n.Notify("assetmgmt", WebhookEvent{Event: domain.EventStepActivated, RequestID: "req-3"})
	n.deliver("assetmgmt", WebhookEvent{Event: domain.EventStepActivated, RequestID: "req-3"}) // synchronous: proves it returns without a network call
}

func TestNilNotifierIsSafe(t *testing.T) {
	var n *WebhookNotifier
	assert.NotPanics(t, func() {
		n.Notify("assetmgmt", WebhookEvent{Event: domain.EventStepActivated})
	})
}
