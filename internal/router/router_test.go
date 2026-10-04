package router

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"approval-engine-service/internal/domain"
	"approval-engine-service/internal/handler"
)

// These tests pin down who may call what. They go through the real router
// and middleware, with fakes standing in for the database.

type fakeApps map[string]string // api key -> app id

func (f fakeApps) GetByAPIKey(_ context.Context, key string) (string, bool, error) {
	id, ok := f[key]
	return id, ok, nil
}

type fakeRequests map[string]*domain.ApprovalRequest

func (f fakeRequests) GetByID(_ context.Context, id string) (*domain.ApprovalRequest, error) {
	return f[id], nil
}

func (f fakeRequests) FindByResource(context.Context, string, string, string) (*domain.ApprovalRequest, error) {
	return nil, nil
}

type fakeParticipants struct{ imported int }

func (f *fakeParticipants) UpsertBatch(_ context.Context, p []domain.Participant) error {
	f.imported += len(p)
	return nil
}

func (f *fakeParticipants) List(context.Context, int, int) ([]domain.Participant, int, error) {
	return nil, 0, nil
}

func newTestApp(adminKey string, portalEnabled bool) (*fiber.App, *fakeParticipants) {
	requests := fakeRequests{
		"req-a": {ID: "req-a", AppID: "app-a", Status: domain.StatusPending},
		"req-b": {ID: "req-b", AppID: "app-b", Status: domain.StatusPending},
	}
	participants := &fakeParticipants{}

	app := fiber.New()
	Register(app, Dependencies{
		Health:        handler.NewHealthHandler(),
		Requests:      handler.NewRequestHandler(nil, requests),
		Participants:  handler.NewParticipantHandler(participants),
		APIKeyLookup:  fakeApps{"key-a": "app-a", "key-b": "app-b"},
		AdminAPIKey:   adminKey,
		PortalEnabled: portalEnabled,
	})
	return app, participants
}

func call(t *testing.T, app *fiber.App, method, path string, headers map[string]string, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

func TestApplicationRoutesRequireAPIKey(t *testing.T) {
	app, _ := newTestApp("", true)
	decision := `{"user_id":"EMP1","decision":"approved"}`

	status, _ := call(t, app, http.MethodGet, "/api/v1/requests/req-a", nil, "")
	assert.Equal(t, fiber.StatusUnauthorized, status, "reading a request needs a key")

	status, _ = call(t, app, http.MethodPost, "/api/v1/requests/req-a/decision", nil, decision)
	assert.Equal(t, fiber.StatusUnauthorized, status, "deciding needs a key")

	status, _ = call(t, app, http.MethodGet, "/api/v1/requests/req-a", map[string]string{"X-API-Key": "wrong"}, "")
	assert.Equal(t, fiber.StatusUnauthorized, status, "an unknown key is rejected")
}

func TestApplicationOnlyReachesItsOwnRequests(t *testing.T) {
	app, _ := newTestApp("", true)
	keyA := map[string]string{"X-API-Key": "key-a"}

	status, _ := call(t, app, http.MethodGet, "/api/v1/requests/req-a", keyA, "")
	assert.Equal(t, fiber.StatusOK, status, "an app reads its own request")

	status, body := call(t, app, http.MethodGet, "/api/v1/requests/req-b", keyA, "")
	assert.Equal(t, fiber.StatusNotFound, status, "another app's request looks like it does not exist")
	assert.NotContains(t, body, "app-b")

	status, _ = call(t, app, http.MethodPost, "/api/v1/requests/req-b/decision", keyA, `{"user_id":"EMP1","decision":"approved"}`)
	assert.Equal(t, fiber.StatusNotFound, status, "an app cannot decide another app's request")
}

func TestParticipantImportNeedsAdminKey(t *testing.T) {
	batch := `{"participants":[{"user_id":"EMP1","name":"One","is_active":true}]}`

	app, store := newTestApp("", true)
	status, _ := call(t, app, http.MethodPost, "/api/v1/participants/import", map[string]string{"X-Admin-Key": "anything"}, batch)
	assert.Equal(t, fiber.StatusServiceUnavailable, status, "no admin key configured means the endpoint is off, not open")
	assert.Zero(t, store.imported)

	app, store = newTestApp("admin-secret", true)
	status, _ = call(t, app, http.MethodPost, "/api/v1/participants/import", map[string]string{"X-API-Key": "key-a"}, batch)
	assert.Equal(t, fiber.StatusUnauthorized, status, "an application key cannot rewrite the shared org chart")

	status, _ = call(t, app, http.MethodPost, "/api/v1/participants/import", map[string]string{"X-Admin-Key": "wrong"}, batch)
	assert.Equal(t, fiber.StatusUnauthorized, status)
	assert.Zero(t, store.imported)

	status, _ = call(t, app, http.MethodPost, "/api/v1/participants/import", map[string]string{"X-Admin-Key": "admin-secret"}, batch)
	assert.Equal(t, fiber.StatusOK, status)
	assert.Equal(t, 1, store.imported)
}

func TestPortalGate(t *testing.T) {
	app, _ := newTestApp("", true)
	status, _ := call(t, app, http.MethodGet, "/api/v1/portal/requests/req-b", nil, "")
	assert.Equal(t, fiber.StatusOK, status, "the enabled portal reads any request without an app key")

	app, _ = newTestApp("", false)
	status, _ = call(t, app, http.MethodGet, "/api/v1/portal/requests/req-b", nil, "")
	assert.Equal(t, fiber.StatusForbidden, status, "PORTAL_ENABLED=false shuts the portal routes")
	status, _ = call(t, app, http.MethodPost, "/api/v1/portal/requests/req-b/decision", nil, `{"user_id":"EMP1","decision":"approved"}`)
	assert.Equal(t, fiber.StatusForbidden, status)
}

func TestOldOpenRoutesAreGone(t *testing.T) {
	app, _ := newTestApp("", true)
	for _, path := range []string{"/api/v1/inbox/EMP1", "/api/v1/workflows", "/api/v1/applications", "/api/v1/participants"} {
		status, _ := call(t, app, http.MethodGet, path, nil, "")
		assert.Equal(t, fiber.StatusNotFound, status, "%s must no longer be reachable outside /portal", path)
	}
}
