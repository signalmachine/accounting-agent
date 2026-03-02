package web

import (
	"net/http"
	"time"

	"accounting-agent/internal/app"
	"accounting-agent/web/templates/layouts"
	"accounting-agent/web/templates/pages"

	"github.com/golang-jwt/jwt/v5"
)

// ── Login page ────────────────────────────────────────────────────────────────

// loginPage handles GET /login — renders the sign-in page.
// Redirects to /dashboard if already authenticated.
func (h *Handler) loginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, redirect to dashboard.
	if cookie, err := r.Cookie("auth_token"); err == nil {
		claims := &jwtClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(h.jwtSecret), nil
		})
		if err == nil && token.Valid {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.Login("").Render(r.Context(), w)
}

// loginFormSubmit handles POST /login — form-based login.
func (h *Handler) loginFormSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.Login("Invalid form submission.").Render(r.Context(), w)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	session, err := h.svc.AuthenticateUser(r.Context(), username, password)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.Login("Invalid username or password.").Render(r.Context(), w)
		return
	}

	claims := &jwtClaims{
		UserID:      session.UserID,
		CompanyID:   session.CompanyID,
		CompanyCode: session.CompanyCode,
		Username:    session.Username,
		Role:        session.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.Login("Server error. Please try again.").Render(r.Context(), w)
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
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// registerPage handles GET /register — renders the company registration form.
// Redirects to dashboard if already authenticated.
func (h *Handler) registerPage(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("auth_token"); err == nil {
		claims := &jwtClaims{}
		token, err := jwt.ParseWithClaims(cookie.Value, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(h.jwtSecret), nil
		})
		if err == nil && token.Valid {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.Register("").Render(r.Context(), w)
}

// registerFormSubmit handles POST /register — creates a new company + admin user and issues a JWT.
func (h *Handler) registerFormSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.Register("Invalid form submission.").Render(r.Context(), w)
		return
	}

	session, err := h.svc.RegisterCompany(r.Context(), app.RegisterCompanyRequest{
		CompanyName: r.FormValue("company_name"),
		Username:    r.FormValue("username"),
		Email:       r.FormValue("email"),
		Password:    r.FormValue("password"),
	})
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.Register(err.Error()).Render(r.Context(), w)
		return
	}

	claims := &jwtClaims{
		UserID:      session.UserID,
		CompanyID:   session.CompanyID,
		CompanyCode: session.CompanyCode,
		Username:    session.Username,
		Role:        session.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.jwtSecret))
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.Register("Server error. Please try again.").Render(r.Context(), w)
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
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logoutPage handles POST /logout — clears cookie and redirects to login.
func (h *Handler) logoutPage(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ── Dashboard ─────────────────────────────────────────────────────────────────

// dashboardPage handles GET /dashboard.
func (h *Handler) dashboardPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Dashboard", "dashboard")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.Dashboard(d).Render(r.Context(), w)
}

// ── About ─────────────────────────────────────────────────────────────────────

// aboutPage handles GET /about.
func (h *Handler) aboutPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "About", "about")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.About(d).Render(r.Context(), w)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// buildAppLayoutData constructs AppLayoutData from the request context.
// All fields are read directly from the JWT claims — no database round trip.
func (h *Handler) buildAppLayoutData(r *http.Request, title, activeNav string) layouts.AppLayoutData {
	claims := authFromContext(r.Context())
	username, role, companyCode := "", "", ""
	if claims != nil {
		username = claims.Username
		role = claims.Role
		companyCode = claims.CompanyCode
	}

	companyName := "Accounting"
	if companyCode != "" {
		companyName = companyCode
	}

	return layouts.AppLayoutData{
		Title:       title,
		CompanyName: companyName,
		CompanyCode: companyCode,
		FYBadge:     "FY 2025-26",
		Username:    username,
		Role:        role,
		ActiveNav:   activeNav,
	}
}
