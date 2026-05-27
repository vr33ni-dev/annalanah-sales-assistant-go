package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Session struct {
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Exp   time.Time `json:"exp"`
}

type Auth struct {
	OAuth      *oauth2.Config
	Allowed    map[string]bool
	CookieName string
	CookieKey  []byte
}

func (a *Auth) sign(b []byte) string {
	h := hmac.New(sha256.New, a.CookieKey)
	h.Write(b)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func IsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return true
	}
	if strings.HasPrefix(os.Getenv("OAUTH_REDIRECT_URL"), "https://") {
		return true
	}
	if strings.HasPrefix(os.Getenv("POST_LOGIN_REDIRECT"), "https://") {
		return true
	}
	return false
}

func (a *Auth) MakeCookie(sess Session, secure bool) *http.Cookie {
	payload, _ := json.Marshal(sess)
	enc := base64.RawURLEncoding.EncodeToString(payload)
	token := enc + "." + a.sign([]byte(enc))
	sameSite := http.SameSiteLaxMode
	useSecure := secure
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_CROSS_SITE_COOKIES")), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_CROSS_SITE_COOKIES")), "true") {
		sameSite = http.SameSiteNoneMode
		useSecure = true
	}
	return &http.Cookie{
		Name:     a.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   useSecure,
		SameSite: sameSite,
		Expires:  sess.Exp,
	}
}

func RandState() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func (a *Auth) ParseSession(r *http.Request) (*Session, bool) {
	if a == nil || a.CookieName == "" {
		return nil, false
	}
	c, err := r.Cookie(a.CookieName)
	if err != nil {
		return nil, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return nil, false
	}
	mac := hmac.New(sha256.New, a.CookieKey)
	mac.Write([]byte(parts[0]))
	expected, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(mac.Sum(nil), expected) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	var s Session
	if json.Unmarshal(raw, &s) != nil || time.Now().After(s.Exp) {
		return nil, false
	}
	return &s, true
}

// NewAuth constructs an Auth from environment variables.
func NewAuth() (*Auth, error) {
	allowed := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("ALLOWED_EMAILS"), ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" {
			allowed[e] = true
		}
	}
	key := []byte(os.Getenv("COOKIE_SIGNING_KEY"))
	if len(key) < 32 {
		return nil, errors.New("COOKIE_SIGNING_KEY must be >=32 bytes")
	}
	return &Auth{
		OAuth: &oauth2.Config{
			ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
			RedirectURL:  os.Getenv("OAUTH_REDIRECT_URL"),
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
		Allowed:    allowed,
		CookieName: "app_session",
		CookieKey:  key,
	}, nil
}

// Test helpers
func (a *Auth) MakeCookieForTest(sess Session, secure bool) *http.Cookie { return a.MakeCookie(sess, secure) }
func IsSecureForTest(r *http.Request) bool                                { return IsSecure(r) }
func RandStateForTest() string                                            { return RandState() }
