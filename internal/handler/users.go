package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/user/jobifai/internal/auth"
)

// UserHandlers groups user account and auth handlers.
type UserHandlers struct {
	svc   *Services
	users *auth.UserStore
	tm    *auth.TokenManager
	db    *sql.DB
}

func NewUserHandlers(svc *Services, users *auth.UserStore, tm *auth.TokenManager, db *sql.DB) *UserHandlers {
	return &UserHandlers{svc: svc, users: users, tm: tm, db: db}
}

// POST /auth/register
func (h *UserHandlers) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "email and password are required"})
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "password must be at least 8 characters"})
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not hash password"})
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.Email
	}
	user, err := h.users.Create(req.Email, hash, req.DisplayName)
	if errors.Is(err, auth.ErrEmailTaken) {
		writeJSON(w, http.StatusConflict, map[string]string{"message": "email already registered"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	tokens, err := h.issueTokens(user.ID, user.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not issue tokens"})
		return
	}
	writeJSON(w, http.StatusCreated, tokens)
}

// POST /auth/login
func (h *UserHandlers) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	user, err := h.users.ByEmail(req.Email)
	if errors.Is(err, auth.ErrUserNotFound) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	if user.PasswordHash == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "this account uses Google sign-in"})
		return
	}
	if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "invalid credentials"})
		return
	}

	tokens, err := h.issueTokens(user.ID, user.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not issue tokens"})
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// POST /auth/refresh
func (h *UserHandlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	if req.RefreshToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "refresh_token is required"})
		return
	}

	userID, err := auth.ConsumeRefreshToken(h.db, req.RefreshToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"message": err.Error()})
		return
	}

	user, err := h.users.ByID(userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "user not found"})
		return
	}

	tokens, err := h.issueTokens(user.ID, user.Email)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": "could not issue tokens"})
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// POST /auth/logout
func (h *UserHandlers) Logout(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	// Best-effort decode, even if the body is missing we still return 200.
	_ = json.NewDecoder(r.Body).Decode(&req)
	userID := auth.UserIDFromCtx(r.Context())
	if userID != "" {
		_ = auth.RevokeAllRefreshTokens(h.db, userID)
	} else if req.RefreshToken != "" {
		if uid, err := auth.ConsumeRefreshToken(h.db, req.RefreshToken); err == nil {
			_ = auth.RevokeAllRefreshTokens(h.db, uid)
		}
	}
	okMsg(w, "logged out")
}

// GET /api/me
func (h *UserHandlers) Me(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	user, err := h.users.ByID(userID)
	if errors.Is(err, auth.ErrUserNotFound) {
		notFound(w, "user not found")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           user.ID,
		"email":        user.Email,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"has_password": user.PasswordHash != "",
		"has_google":   user.GoogleID != "",
		"is_admin":     user.IsAdmin,
	})
}

// PUT /api/me
func (h *UserHandlers) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	var req struct {
		DisplayName string `json:"display_name"`
		AvatarURL   string `json:"avatar_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "invalid JSON"})
		return
	}
	if err := h.users.Update(userID, strings.TrimSpace(req.DisplayName), strings.TrimSpace(req.AvatarURL)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}
	okMsg(w, "profile updated")
}

// issueTokens creates a new access + refresh token pair for the user.
func (h *UserHandlers) issueTokens(userID, email string) (*auth.Tokens, error) {
	accessToken, err := h.tm.IssueAccess(userID, email)
	if err != nil {
		return nil, err
	}
	rawRefresh, err := auth.GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	if err := auth.SaveRefreshToken(h.db, userID, rawRefresh); err != nil {
		return nil, err
	}
	return &auth.Tokens{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresIn:    int(auth.AccessTokenTTL().Seconds()),
	}, nil
}
