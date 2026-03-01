package web

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type authClaimsKey struct{}

// AuthClaims holds the authenticated user's identity extracted from the JWT.
type AuthClaims struct {
	UserID    int
	CompanyID int
	Role      string
}

// authFromContext returns the auth claims stored in ctx, or nil.
func authFromContext(ctx context.Context) *AuthClaims {
	v, _ := ctx.Value(authClaimsKey{}).(*AuthClaims)
	return v
}

// jwtClaims is the JWT payload struct used for signing and parsing.
type jwtClaims struct {
	UserID    int    `json:"user_id"`
	CompanyID int    `json:"company_id"`
	Role      string `json:"role"`
	jwt.RegisteredClaims
}

// RequireAuth is chi middleware that validates the auth_token cookie and injects
// AuthClaims into the request context. Returns 401 if the token is absent or invalid.
func (h *Handler) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			writeError(w, r, "authentication required", "UNAUTHORIZED", http.StatusUnauthorized)
			return
		}

		claims := &jwtClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(h.jwtSecret), nil
		})
		if err != nil || !token.Valid {
			writeError(w, r, "invalid or expired token", "UNAUTHORIZED", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), authClaimsKey{}, &AuthClaims{
			UserID:    claims.UserID,
			CompanyID: claims.CompanyID,
			Role:      claims.Role,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuthBrowser is middleware for HTML page routes. Unlike RequireAuth (which returns 401 JSON),
// this middleware redirects unauthenticated requests to /login with a 302.
func (h *Handler) RequireAuthBrowser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		claims := &jwtClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(h.jwtSecret), nil
		})
		if err != nil || !token.Valid {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		ctx := context.WithValue(r.Context(), authClaimsKey{}, &AuthClaims{
			UserID:    claims.UserID,
			CompanyID: claims.CompanyID,
			Role:      claims.Role,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// login handles POST /api/auth/login.
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	session, err := h.svc.AuthenticateUser(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, r, "invalid username or password", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	claims := &jwtClaims{
		UserID:    session.UserID,
		CompanyID: session.CompanyID,
		Role:      session.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		writeError(w, r, "token generation failed", "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	})
	writeJSON(w, session)
}

// logout handles POST /api/auth/logout — clears the auth cookie.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

// me handles GET /api/auth/me — returns the current user's profile.
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims := authFromContext(r.Context())
	if claims == nil {
		writeError(w, r, "not authenticated", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	user, err := h.svc.GetUser(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, r, "user not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	type meResponse struct {
		Username    string `json:"username"`
		Role        string `json:"role"`
		CompanyCode string `json:"company_code"`
	}
	writeJSON(w, meResponse{
		Username:    user.Username,
		Role:        user.Role,
		CompanyCode: user.CompanyCode,
	})
}
