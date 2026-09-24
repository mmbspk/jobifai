package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleConfig holds the OAuth2 application credentials.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Endpoint overrides OAuth URLs (used in tests with httptest servers). Zero value → google.Endpoint.
	Endpoint oauth2.Endpoint
	// UserInfoURL overrides the Google profile endpoint (tests only).
	UserInfoURL string
}

// GoogleHandler handles the Google OAuth2 flow.
type GoogleHandler struct {
	cfg        *oauth2.Config
	db         *sql.DB
	tm         *TokenManager
	userInfoURL string
	onUpsert   func(ctx context.Context, googleID, email, name, avatar string) (string, error)
}

// NewGoogleHandler creates a GoogleHandler.
// onUpsert is called after a successful Google auth; it receives Google profile
// fields and must return the app's user_id (creating or fetching the user).
func NewGoogleHandler(
	gcfg GoogleConfig,
	db *sql.DB,
	tm *TokenManager,
	onUpsert func(ctx context.Context, googleID, email, name, avatar string) (string, error),
) *GoogleHandler {
	ep := gcfg.Endpoint
	if ep.AuthURL == "" {
		ep = google.Endpoint
	}
	cfg := &oauth2.Config{
		ClientID:     gcfg.ClientID,
		ClientSecret: gcfg.ClientSecret,
		RedirectURL:  gcfg.RedirectURL,
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint:     ep,
	}
	return &GoogleHandler{cfg: cfg, db: db, tm: tm, userInfoURL: gcfg.UserInfoURL, onUpsert: onUpsert}
}

// Redirect redirects the user to Google's consent page.
// GET /auth/google
func (h *GoogleHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		log.Error().Err(err).Msg("oauth: failed to generate state")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Store state in a short-lived cookie for CSRF protection.
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.cfg.AuthCodeURL(state, oauth2.AccessTypeOffline), http.StatusFound)
}

// Callback handles Google's redirect back to the app.
// GET /auth/google/callback
func (h *GoogleHandler) Callback(w http.ResponseWriter, r *http.Request) {
	// Validate CSRF state.
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	// Clear the state cookie.
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})

	code := r.URL.Query().Get("code")
	oauthToken, err := h.cfg.Exchange(r.Context(), code)
	if err != nil {
		log.Error().Err(err).Msg("google token exchange failed")
		http.Error(w, "oauth token exchange failed", http.StatusInternalServerError)
		return
	}

	// Fetch user info from Google.
	profile, err := fetchGoogleProfile(r.Context(), h.cfg, oauthToken, h.userInfoURL)
	if err != nil {
		log.Error().Err(err).Msg("google profile fetch failed")
		http.Error(w, "failed to fetch google profile", http.StatusInternalServerError)
		return
	}

	userID, err := h.onUpsert(r.Context(), profile.Sub, profile.Email, profile.Name, profile.Picture)
	if err != nil {
		log.Error().Err(err).Msg("user upsert failed")
		http.Error(w, "user upsert failed", http.StatusInternalServerError)
		return
	}

	accessToken, err := h.tm.IssueAccess(userID, profile.Email)
	if err != nil {
		http.Error(w, "token issue failed", http.StatusInternalServerError)
		return
	}

	refreshToken, err := GenerateRefreshToken()
	if err != nil {
		http.Error(w, "token generation failed", http.StatusInternalServerError)
		return
	}
	if err := SaveRefreshToken(h.db, userID, refreshToken); err != nil {
		log.Error().Err(err).Msg("store refresh token")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Redirect to frontend with tokens in URL fragment — never in query string.
	redirectURL := fmt.Sprintf("/#access_token=%s&refresh_token=%s", accessToken, refreshToken)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

type googleProfile struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func fetchGoogleProfile(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token, userInfoURL string) (*googleProfile, error) {
	if userInfoURL == "" {
		userInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
	}
	client := cfg.Client(ctx, token)
	resp, err := client.Get(userInfoURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var p googleProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	// Behind Caddy / other reverse proxies.
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	return false
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand unavailable: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
