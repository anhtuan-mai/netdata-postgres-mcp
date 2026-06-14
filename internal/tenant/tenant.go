// SPDX-License-Identifier: GPL-3.0-or-later

// Package tenant provides multi-tenant token resolution middleware.
package tenant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey string

const tenantKey contextKey = "tenant"

// Info holds resolved tenant information from a token lookup.
type Info struct {
	TenantID   string   `json:"tenant_id"`
	TenantName string   `json:"tenant_name"`
	Label      string   `json:"label"`
	Scopes     []string `json:"scopes"` // allowed node_id patterns; empty = all
}

// FromContext extracts tenant info from the request context.
// Returns nil if no tenant is attached (single-tenant mode).
func FromContext(ctx context.Context) *Info {
	v, _ := ctx.Value(tenantKey).(*Info)
	return v
}

// Middleware resolves bearer tokens to tenants via database lookup.
// If multiTenant is false, the middleware is a no-op passthrough.
func Middleware(pool *pgxpool.Pool, multiTenant bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !multiTenant {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "missing bearer token",
				})
				return
			}

			info, err := resolveToken(r.Context(), pool, token)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{
					"error": "invalid or expired token",
				})
				return
			}

			ctx := context.WithValue(r.Context(), tenantKey, info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HashToken returns the SHA-256 hex digest of a raw bearer token.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func resolveToken(ctx context.Context, pool *pgxpool.Pool, rawToken string) (*Info, error) {
	hash := HashToken(rawToken)

	var info Info
	var expiresAt *time.Time

	err := pool.QueryRow(ctx, `
		SELECT t.id, t.name, at.label, at.scopes, at.expires_at
		FROM api_tokens at
		JOIN tenants t ON t.id = at.tenant_id
		WHERE at.token_hash = $1
	`, hash).Scan(&info.TenantID, &info.TenantName, &info.Label, &info.Scopes, &expiresAt)
	if err != nil {
		return nil, err
	}

	if expiresAt != nil && expiresAt.Before(time.Now()) {
		return nil, err
	}

	return &info, nil
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fallback to query parameter
	return r.URL.Query().Get("token")
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
