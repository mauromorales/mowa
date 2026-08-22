package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// runThroughMiddleware wires apiKeyMiddleware around a handler that always
// succeeds, and returns the response recorded for a request carrying the
// given Authorization header value ("" for none).
func runThroughMiddleware(t *testing.T, cfg *Config, authHeader string) *httptest.ResponseRecorder {
	t.Helper()

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/uptime", nil)
	if authHeader != "" {
		req.Header.Set(echo.HeaderAuthorization, authHeader)
	}
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	handler := apiKeyMiddleware(cfg)(func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})
	if err := handler(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}
	return rec
}

// TestAPIKeyMiddlewareNoKeyConfiguredAllowsAnyRequest matches the documented
// (and intentional, see issue #19) behavior: an unset api_key means the API
// stays open, not that requests are rejected by default.
func TestAPIKeyMiddlewareNoKeyConfiguredAllowsAnyRequest(t *testing.T) {
	rec := runThroughMiddleware(t, &Config{APIKey: ""}, "")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with no api_key configured, got %d", rec.Code)
	}
}

// TestAPIKeyMiddlewareRejectsMissingKey ensures a configured key is actually
// enforced when the caller sends no Authorization header at all.
func TestAPIKeyMiddlewareRejectsMissingKey(t *testing.T) {
	rec := runThroughMiddleware(t, &Config{APIKey: "secret"}, "")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with no Authorization header, got %d", rec.Code)
	}
}

// TestAPIKeyMiddlewareRejectsWrongKey ensures a present but incorrect key is
// rejected, not just an absent one.
func TestAPIKeyMiddlewareRejectsWrongKey(t *testing.T) {
	rec := runThroughMiddleware(t, &Config{APIKey: "secret"}, "wrong-key")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with a wrong key, got %d", rec.Code)
	}
}

// TestAPIKeyMiddlewareAcceptsCorrectKeyRaw confirms the raw header value is
// compared directly, no "Bearer " prefix expected, matching the ApiKeyAuth
// scheme already declared in main.go's Swagger annotations.
func TestAPIKeyMiddlewareAcceptsCorrectKeyRaw(t *testing.T) {
	rec := runThroughMiddleware(t, &Config{APIKey: "secret"}, "secret")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 with the correct raw key, got %d", rec.Code)
	}
}

// TestAPIKeyMiddlewareBearerPrefixIsNotStripped documents that "Bearer secret"
// does not match a configured key of "secret": this is an apiKey scheme, not
// OAuth Bearer, so no prefix stripping happens. A caller must send the raw key.
func TestAPIKeyMiddlewareBearerPrefixIsNotStripped(t *testing.T) {
	rec := runThroughMiddleware(t, &Config{APIKey: "secret"}, "Bearer secret")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for a Bearer-prefixed header against a raw key, got %d", rec.Code)
	}
}
