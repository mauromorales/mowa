package main

import (
	"crypto/subtle"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
)

// apiKeyMiddleware enforces the configured api_key on every request it wraps,
// matching the ApiKeyAuth scheme already declared in the Swagger annotations
// in main.go (@in header, @name Authorization): the raw key is the header
// value, no "Bearer " prefix.
//
// When cfg.APIKey is empty, mowa is intentionally reachable without a
// credential (its own network exposure is by design, see issue #19), but
// that is worth calling out loudly at startup rather than leaving it silent.
func apiKeyMiddleware(cfg *Config) echo.MiddlewareFunc {
	if cfg.APIKey == "" {
		log.Println("⚠️  No api_key configured; every /api endpoint is reachable by anyone who can reach this host, with no credential. Set api_key in config.yaml to require one.")
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if cfg.APIKey == "" {
				return next(c)
			}

			got := c.Request().Header.Get(echo.HeaderAuthorization)
			// Constant-time compare so a valid key can't be recovered by timing
			// how quickly a byte-by-byte mismatch is rejected.
			if subtle.ConstantTimeCompare([]byte(got), []byte(cfg.APIKey)) != 1 {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or missing API key")
			}
			return next(c)
		}
	}
}
