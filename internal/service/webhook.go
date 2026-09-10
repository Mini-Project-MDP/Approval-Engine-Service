package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"approval-engine-service/internal/domain"
)

// WebhookEvent is the payload POSTed to a consuming application's
// callback_url whenever one of its requests/steps changes state. It mirrors
// the audit event the engine already writes to approval_events, just
// addressed to the application instead of the database.
//
// It is a notification, not the source of truth: GET /requests/{id} always
// has the authoritative, current state, so an app that misses a delivery
// (or is down when one is attempted) loses nothing but timeliness — it can
// always fall back to that endpoint to catch up.
//
// Deliveries for the same request are NOT guaranteed to arrive in the order
// the events happened (each is retried independently in its own goroutine,
// so a later event can win a race and land first). This is deliberate
// rather than a gap to fix: a consumer should treat receipt of ANY webhook
// as "something changed, go GET /requests/{id} to see the current state",
// not accumulate meaning from the event stream's order. Under that pattern
// — which the idempotent, always-current GET response is built for —
// out-of-order arrival never produces a wrong result, only a redundant
// re-fetch.
type WebhookEvent struct {
	Event      string         `json:"event"`
	AppID      string         `json:"app_id"`
	RequestID  string         `json:"request_id"`
	StepID     *string        `json:"step_id,omitempty"`
	ActorID    *string        `json:"actor_id,omitempty"`
	Detail     map[string]any `json:"detail,omitempty"`
	OccurredAt string         `json:"occurred_at"`
}

// WebhookApplicationLookup is the slice of application data the notifier
// needs: where to deliver (CallbackURL) and what secret to sign with
// (APIKey). *repository.ApplicationRepository satisfies this.
type WebhookApplicationLookup interface {
	GetByID(ctx context.Context, id string) (*domain.Application, error)
}

// WebhookNotifier delivers WebhookEvents in the background. Every call is
// fire-and-forget from the engine's point of view: a slow or unreachable
// consumer callback must never slow down or fail an approval decision, so
// Notify always returns immediately and delivery failures are only logged.
type WebhookNotifier struct {
	apps   WebhookApplicationLookup
	client *http.Client
}

func NewWebhookNotifier(apps WebhookApplicationLookup) *WebhookNotifier {
	return &WebhookNotifier{apps: apps, client: &http.Client{Timeout: 5 * time.Second}}
}

// retryBackoff is how long to wait before each delivery attempt (first is
// immediate). Three tries total is enough to ride out a brief blip without
// holding the event loop open for long.
var retryBackoff = []time.Duration{0, time.Second, 3 * time.Second}

// Notify looks up appID's callback_url and, if one is configured, delivers
// event in a background goroutine detached from the caller's context (the
// HTTP request that triggered this event will usually have already
// finished responding by the time delivery completes).
func (n *WebhookNotifier) Notify(appID string, event WebhookEvent) {
	if n == nil {
		return
	}
	go n.deliver(appID, event)
}

func (n *WebhookNotifier) deliver(appID string, event WebhookEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	app, err := n.apps.GetByID(ctx, appID)
	if err != nil {
		log.Printf("webhook: look up application %s: %v", appID, err)
		return
	}
	if app == nil || app.CallbackURL == "" {
		return // nothing to deliver to — this app hasn't opted in
	}

	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("webhook: marshal event for app %s: %v", appID, err)
		return
	}
	signature := sign(body, app.APIKey)

	var lastErr error
	for _, wait := range retryBackoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		if lastErr = n.attempt(ctx, app.CallbackURL, body, signature); lastErr == nil {
			return
		}
	}
	log.Printf("webhook: delivery to %s failed after %d attempts (app %s, event %s): %v",
		app.CallbackURL, len(retryBackoff), appID, event.Event, lastErr)
}

// attempt makes one delivery. The receiving app should verify
// X-Webhook-Signature (hex HMAC-SHA256 of the raw body, keyed with the
// api_key it was issued) before trusting the payload, since the callback
// URL itself has no other authentication.
func (n *WebhookNotifier) attempt(ctx context.Context, url string, body []byte, signature string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signature)

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("callback returned status %d", resp.StatusCode)
	}
	return nil
}

func sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
