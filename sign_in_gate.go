package main

// Who may call the API.
//
// Every route under /api/ (with or without the /argraphments prefix) needs a
// signed-in administrator. The one credential is sign-in-store's
// sign_in_session cookie, which sign-in-store sets for its cookie domain and
// every name under it. sign-in-store's GET /session says who the cookie signs
// in and whether principal-store marks them an administrator. No header, no
// other cookie and no source address counts: nginx connects from loopback, so
// the source address says nothing about the visitor.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const signInSessionCookieName = "sign_in_session"

// signInSessionCacheLifetime is how long sign-in-store's answer about one
// cookie is reused. A page load makes several API calls at once.
const signInSessionCacheLifetime = 10 * time.Second

// signInSessionCacheMaximumEntries bounds the cache. Expired entries are
// dropped whenever one is added; past this many live entries a new answer is
// used once and not kept.
const signInSessionCacheMaximumEntries = 1000

type signInSession struct {
	PrincipalID     string `json:"principal_id"`
	DisplayName     string `json:"display_name"`
	IsAdministrator bool   `json:"is_administrator"`
}

type cachedSignInSession struct {
	session *signInSession // nil: the cookie signs nobody in
	readAt  time.Time
}

// SignInGate answers every API request that is not from a signed-in
// administrator, and passes the rest on.
type SignInGate struct {
	signInStoreURL string
	// signInPageURL and cookieDomain are read from sign-in-store's own
	// settings, so the notice a signed-out visitor sees never carries a copy.
	signInPageURL string
	cookieDomain  string
	client        *http.Client
	now           func() time.Time

	cacheMutex sync.Mutex
	cache      map[string]cachedSignInSession
}

// NewSignInGate reads sign-in-store's sign-in page address and cookie domain
// from its GET /settings and refuses to make a gate when it cannot.
func NewSignInGate(ctx context.Context, signInStoreURL string) (*SignInGate, error) {
	gate := &SignInGate{
		signInStoreURL: strings.TrimRight(signInStoreURL, "/"),
		client:         &http.Client{Timeout: 10 * time.Second},
		now:            time.Now,
		cache:          map[string]cachedSignInSession{},
	}
	settings, err := gate.readSignInStoreSettings(ctx)
	if err != nil {
		return nil, err
	}
	gate.signInPageURL = settings["sign_in_page_url"]
	gate.cookieDomain = settings["cookie_domain"]
	if gate.signInPageURL == "" {
		return nil, fmt.Errorf("sign-in-store's GET /settings names no sign_in_page_url")
	}
	if gate.cookieDomain == "" {
		return nil, fmt.Errorf("sign-in-store's GET /settings names no cookie_domain")
	}
	return gate, nil
}

func (gate *SignInGate) readSignInStoreSettings(ctx context.Context) (map[string]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, gate.signInStoreURL+"/settings", nil)
	if err != nil {
		return nil, err
	}
	response, err := gate.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sign-in-store: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("sign-in-store's GET /settings: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sign-in-store answered GET /settings with %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var described struct {
		Settings []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(body, &described); err != nil {
		return nil, fmt.Errorf("sign-in-store's GET /settings is not a settings list: %w", err)
	}
	settings := map[string]string{}
	for _, setting := range described.Settings {
		settings[setting.Key] = setting.Value
	}
	return settings, nil
}

// methodsSafeFromCrossSiteRequests are the methods that change nothing, so
// another site may send them with the visitor's cookie without harm.
var methodsSafeFromCrossSiteRequests = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
}

// fetchSitesAllowedToChangeThings are the Sec-Fetch-Site values a browser
// sends for a request this site's own page made, or one the visitor typed.
var fetchSitesAllowedToChangeThings = map[string]bool{
	"same-origin": true,
	"none":        true,
}

// RequireAdministrator answers 403 to a cross-site request that would change
// something, 401 to a request with no sign-in or an ended one, 403 to someone
// signed in who is not an administrator, and 502 when sign-in-store cannot
// answer. Only a signed-in administrator's request reaches next.
func (gate *SignInGate) RequireAdministrator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !methodsSafeFromCrossSiteRequests[r.Method] {
			if fetchSite := r.Header.Get("Sec-Fetch-Site"); fetchSite != "" && !fetchSitesAllowedToChangeThings[fetchSite] {
				gate.writeRefusal(w, http.StatusForbidden, "cross-site requests may not change anything here")
				return
			}
		}
		cookie, err := r.Cookie(signInSessionCookieName)
		if err != nil {
			gate.writeRefusal(w, http.StatusUnauthorized, "not signed in")
			return
		}
		session, err := gate.sessionOfCookie(r.Context(), cookie.Value)
		if err != nil {
			log.Printf("sign-in check failed: %v", err)
			writeJSONResponse(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if session == nil {
			gate.writeRefusal(w, http.StatusUnauthorized, "the sign-in has ended")
			return
		}
		if !session.IsAdministrator {
			gate.writeRefusal(w, http.StatusForbidden, "only administrators may use argraphments")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeRefusal answers a refused request with what the page needs to tell the
// visitor where to sign in, and on which host the cookie reaches this app.
func (gate *SignInGate) writeRefusal(w http.ResponseWriter, status int, message string) {
	writeJSONResponse(w, status, map[string]string{
		"error":            message,
		"sign_in_page_url": gate.signInPageURL,
		"cookie_domain":    gate.cookieDomain,
	})
}

func (gate *SignInGate) sessionOfCookie(ctx context.Context, cookieValue string) (*signInSession, error) {
	sum := sha256.Sum256([]byte(cookieValue))
	cacheKey := hex.EncodeToString(sum[:])
	gate.cacheMutex.Lock()
	cached, known := gate.cache[cacheKey]
	gate.cacheMutex.Unlock()
	if known && gate.now().Sub(cached.readAt) < signInSessionCacheLifetime {
		return cached.session, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, gate.signInStoreURL+"/session", nil)
	if err != nil {
		return nil, err
	}
	request.AddCookie(&http.Cookie{Name: signInSessionCookieName, Value: cookieValue})
	response, err := gate.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("sign-in-store: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("sign-in-store's GET /session: %w", err)
	}
	var session *signInSession
	switch response.StatusCode {
	case http.StatusOK:
		session = &signInSession{}
		if err := json.Unmarshal(body, session); err != nil || session.PrincipalID == "" {
			return nil, fmt.Errorf("sign-in-store answered GET /session with a body that is not a session: %s", strings.TrimSpace(string(body)))
		}
	case http.StatusUnauthorized:
	default:
		return nil, fmt.Errorf("sign-in-store answered GET /session with %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	gate.remember(cacheKey, session)
	return session, nil
}

// remember keeps one answer, first dropping every expired one, so the cache
// holds at most the answers of the last signInSessionCacheLifetime.
func (gate *SignInGate) remember(cacheKey string, session *signInSession) {
	now := gate.now()
	gate.cacheMutex.Lock()
	defer gate.cacheMutex.Unlock()
	for key, cached := range gate.cache {
		if now.Sub(cached.readAt) >= signInSessionCacheLifetime {
			delete(gate.cache, key)
		}
	}
	if len(gate.cache) >= signInSessionCacheMaximumEntries {
		return
	}
	gate.cache[cacheKey] = cachedSignInSession{session: session, readAt: now}
}

func writeJSONResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
