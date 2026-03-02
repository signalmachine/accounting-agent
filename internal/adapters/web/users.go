package web

import (
	"net/http"
	"net/url"
	"strconv"

	"accounting-agent/internal/app"
	"accounting-agent/web/templates/pages"

	"github.com/go-chi/chi/v5"
)

// usersPage handles GET /settings/users — renders the admin user management page.
func (h *Handler) usersPage(w http.ResponseWriter, r *http.Request) {
	d := h.buildAppLayoutData(r, "Users", "users")

	if fe := r.URL.Query().Get("flash_error"); fe != "" {
		d.FlashMsg = fe
		d.FlashKind = "error"
	}
	if fs := r.URL.Query().Get("flash_success"); fs != "" {
		d.FlashMsg = fs
		d.FlashKind = "success"
	}

	claims := authFromContext(r.Context())
	if d.CompanyCode == "" || claims == nil {
		d.FlashMsg = "Company not resolved — please log in again"
		d.FlashKind = "error"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = pages.UsersList(d, nil, 0).Render(r.Context(), w)
		return
	}

	result, err := h.svc.ListUsers(r.Context(), d.CompanyCode)
	if err != nil {
		d.FlashMsg = "Failed to load users: " + err.Error()
		d.FlashKind = "error"
		result = &app.UsersResult{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pages.UsersList(d, result, claims.UserID).Render(r.Context(), w)
}

// usersCreateAction handles POST /settings/users — HTML form-based user creation (ADMIN only).
func (h *Handler) usersCreateAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?flash_error=invalid+form", http.StatusSeeOther)
		return
	}

	claims := authFromContext(r.Context())
	if claims == nil || claims.CompanyCode == "" {
		http.Redirect(w, r, "/settings/users?flash_error=company+not+found", http.StatusSeeOther)
		return
	}

	_, err := h.svc.CreateUser(r.Context(), app.CreateUserRequest{
		CompanyCode: claims.CompanyCode,
		Username:    r.FormValue("username"),
		Email:       r.FormValue("email"),
		Password:    r.FormValue("password"),
		Role:        r.FormValue("role"),
	})
	if err != nil {
		http.Redirect(w, r, "/settings/users?flash_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings/users?flash_success=User+created+successfully", http.StatusSeeOther)
}

// usersUpdateRoleAction handles POST /settings/users/{id}/role — changes a user's role.
func (h *Handler) usersUpdateRoleAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?flash_error=invalid+form", http.StatusSeeOther)
		return
	}

	claims := authFromContext(r.Context())
	if claims == nil || claims.CompanyCode == "" {
		http.Redirect(w, r, "/settings/users?flash_error=company+not+found", http.StatusSeeOther)
		return
	}

	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Redirect(w, r, "/settings/users?flash_error=invalid+user+id", http.StatusSeeOther)
		return
	}

	// Prevent self-role-change via this form (could lock yourself out).
	if userID == claims.UserID {
		http.Redirect(w, r, "/settings/users?flash_error=You+cannot+change+your+own+role", http.StatusSeeOther)
		return
	}

	err = h.svc.UpdateUserRole(r.Context(), app.UpdateUserRoleRequest{
		CompanyCode: claims.CompanyCode,
		UserID:      userID,
		Role:        r.FormValue("role"),
	})
	if err != nil {
		http.Redirect(w, r, "/settings/users?flash_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings/users?flash_success=Role+updated", http.StatusSeeOther)
}

// usersToggleActiveAction handles POST /settings/users/{id}/active — activates or deactivates a user.
func (h *Handler) usersToggleActiveAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/settings/users?flash_error=invalid+form", http.StatusSeeOther)
		return
	}

	claims := authFromContext(r.Context())
	if claims == nil || claims.CompanyCode == "" {
		http.Redirect(w, r, "/settings/users?flash_error=company+not+found", http.StatusSeeOther)
		return
	}

	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Redirect(w, r, "/settings/users?flash_error=invalid+user+id", http.StatusSeeOther)
		return
	}

	// Prevent self-deactivation.
	if userID == claims.UserID {
		http.Redirect(w, r, "/settings/users?flash_error=You+cannot+deactivate+your+own+account", http.StatusSeeOther)
		return
	}

	active := r.FormValue("active") == "true"
	err = h.svc.SetUserActive(r.Context(), claims.CompanyCode, userID, active)
	if err != nil {
		http.Redirect(w, r, "/settings/users?flash_error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	msg := "User+deactivated"
	if active {
		msg = "User+activated"
	}
	http.Redirect(w, r, "/settings/users?flash_success="+msg, http.StatusSeeOther)
}

// apiListUsers handles GET /api/companies/{code}/users — returns users as JSON (ADMIN only).
func (h *Handler) apiListUsers(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}

	result, err := h.svc.ListUsers(r.Context(), code)
	if err != nil {
		writeError(w, r, err.Error(), "INTERNAL_ERROR", http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// apiCreateUser handles POST /api/companies/{code}/users — creates a user (ADMIN only).
func (h *Handler) apiCreateUser(w http.ResponseWriter, r *http.Request) {
	code := companyCode(r)
	if !h.requireCompanyAccess(w, r, code) {
		return
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	result, err := h.svc.CreateUser(r.Context(), app.CreateUserRequest{
		CompanyCode: code,
		Username:    req.Username,
		Email:       req.Email,
		Password:    req.Password,
		Role:        req.Role,
	})
	if err != nil {
		writeError(w, r, err.Error(), "BAD_REQUEST", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, result)
}
