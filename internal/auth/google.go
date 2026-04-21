package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleConfig holds the OAuth2 application credentials.
type GoogleConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// GoogleHandler handles the Google OAuth2 flow.
type GoogleHandler struct {
	cfg     *oauth2.Config
	db      *sql.DB
	tm      *TokenManager
	onUpsert func(ctx context.Context, googleID, email, name, avatar string) (string, error)
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
	cfg := &oauth2.Config{
		ClientID:     gcfg.ClientID,
		ClientSecret: gcfg.ClientSecret,
		RedirectURL:  gcfg.RedirectURL,
		Scopes:       []string{"openid", "profile", "email"},
		Endpoint:     google.Endpoint,
	}
	return &GoogleHandler{cfg: cfg, db: db, tm: tm, onUpsert: onUpsert}
}

// Redirect redirects the user to Google's consent page.
// GET /auth/google
func (h *GoogleHandler) Redirect(w http.ResponseWriter, r *http.Request) {
	state := generateState()
	// Store state in a short-lived cookie for CSRF protection.
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
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
	profile, err := fetchGoogleProfile(r.Context(), h.cfg, oauthToken)
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

	// Redirect to frontend with tokens in fragment (never in query string).
	redirectURL := fmt.Sprintf("/?access_token=%s&refresh_token=%s", accessToken, refreshToken)
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

type googleProfile struct {
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func fetchGoogleProfile(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*googleProfile, error) {
	client := cfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var p googleProfile
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

func generateState() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return fmt.Sprintf("%x", h[:8])
}
