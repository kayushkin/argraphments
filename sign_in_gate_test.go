package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testAdministratorCookie    = "cookie-of-an-administrator"
	testNonAdministratorCookie = "cookie-of-a-member"
	testSignInPageURL          = "https://example.com/signin"
	testCookieDomain           = "example.com"
)

// fakeSignInStore answers GET /settings and GET /session as sign-in-store
// does, for the cookies it is given.
type fakeSignInStore struct {
	*httptest.Server
	sessionChecks atomic.Int64
}

func newFakeSignInStore(sessionsByCookie map[string]signInSession) *fakeSignInStore {
	fake := &fakeSignInStore{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/settings":
			writeJSONResponse(w, http.StatusOK, map[string]any{"settings": []map[string]string{
				{"key": "sign_in_page_url", "value": testSignInPageURL},
				{"key": "cookie_domain", "value": testCookieDomain},
			}})
		case "/session":
			fake.sessionChecks.Add(1)
			cookie, err := r.Cookie(signInSessionCookieName)
			if err != nil {
				writeJSONResponse(w, http.StatusUnauthorized, map[string]string{"code": "not_signed_in"})
				return
			}
			session, known := sessionsByCookie[cookie.Value]
			if !known {
				writeJSONResponse(w, http.StatusUnauthorized, map[string]string{"code": "session_unknown"})
				return
			}
			writeJSONResponse(w, http.StatusOK, session)
		default:
			http.NotFound(w, r)
		}
	}))
	return fake
}

func newTestSignInGate(signInStoreURL string) *SignInGate {
	gate, err := NewSignInGate(context.Background(), signInStoreURL)
	if err != nil {
		panic("test sign-in gate: " + err.Error())
	}
	return gate
}

// newGatedTestServer is the real route table behind a gate whose fake
// sign-in-store knows one administrator and one member who is not.
func newGatedTestServer(t *testing.T) (http.Handler, *fakeSignInStore) {
	t.Helper()
	signInStore := newFakeSignInStore(map[string]signInSession{
		testAdministratorCookie:    {PrincipalID: "principal_000001", DisplayName: "Test Administrator", IsAdministrator: true},
		testNonAdministratorCookie: {PrincipalID: "principal_000002", DisplayName: "Test Member"},
	})
	t.Cleanup(signInStore.Close)
	return newServeMux(newTestSignInGate(signInStore.URL)), signInStore
}

func serve(handler http.Handler, method, path, cookieValue string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if cookieValue != "" {
		request.AddCookie(&http.Cookie{Name: signInSessionCookieName, Value: cookieValue})
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestAnonymousAPICallsAreRefusedWith401(t *testing.T) {
	setupTestStore(t)
	handler, _ := newGatedTestServer(t)
	cases := []struct{ method, path string }{
		{"GET", "/api/transcripts"},
		{"GET", "/argraphments/api/transcripts"},
		{"GET", "/api/transcripts/bold-fox"},
		{"GET", "/api/graph"},
		{"GET", "/api/speakers"},
		{"PUT", "/argraphments/api/speakers/Alice"},
		{"POST", "/api/analyze"},
		{"POST", "/api/transcribe"},
		{"POST", "/argraphments/api/import/youtube"},
		{"POST", "/api/sample"},
		{"GET", "/api/no-such-route"},
	}
	for _, c := range cases {
		recorder := serve(handler, c.method, c.path, "", nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with no cookie: got %d, want 401", c.method, c.path, recorder.Code)
			continue
		}
		var body map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s: refusal is not JSON: %v", c.method, c.path, err)
		}
		if body["sign_in_page_url"] != testSignInPageURL || body["cookie_domain"] != testCookieDomain {
			t.Errorf("%s %s: refusal names %q and %q, want sign-in-store's settings", c.method, c.path, body["sign_in_page_url"], body["cookie_domain"])
		}
	}
}

func TestUnknownCookieIsRefusedWith401(t *testing.T) {
	setupTestStore(t)
	handler, _ := newGatedTestServer(t)
	if code := serve(handler, "GET", "/api/transcripts", "made-up-cookie", nil).Code; code != http.StatusUnauthorized {
		t.Fatalf("made-up cookie: got %d, want 401", code)
	}
}

func TestNonAdministratorIsRefusedWith403(t *testing.T) {
	setupTestStore(t)
	handler, _ := newGatedTestServer(t)
	for _, method := range []string{"GET", "PUT"} {
		if code := serve(handler, method, "/api/speakers/Alice", testNonAdministratorCookie, nil).Code; code != http.StatusForbidden {
			t.Errorf("%s as a member who is not an administrator: got %d, want 403", method, code)
		}
	}
}

func TestAdministratorIsLetThrough(t *testing.T) {
	setupTestStore(t)
	handler, _ := newGatedTestServer(t)
	if _, err := store.SaveTranscript("a transcript", "a title"); err != nil {
		t.Fatal(err)
	}
	recorder := serve(handler, "GET", "/argraphments/api/transcripts", testAdministratorCookie, nil)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "a title") {
		t.Fatalf("administrator: got %d %q, want 200 and the saved transcript", recorder.Code, recorder.Body.String())
	}
	if code := serve(handler, "GET", "/api/no-such-route", testAdministratorCookie, nil).Code; code != http.StatusNotFound {
		t.Fatalf("administrator on an unknown API route: got %d, want 404", code)
	}
}

func TestSignInStoreDownIsA502(t *testing.T) {
	setupTestStore(t)
	signInStore := newFakeSignInStore(nil)
	gate := newTestSignInGate(signInStore.URL)
	signInStore.Close()
	handler := newServeMux(gate)
	if code := serve(handler, "GET", "/api/transcripts", testAdministratorCookie, nil).Code; code != http.StatusBadGateway {
		t.Fatalf("sign-in-store down: got %d, want 502", code)
	}
}

func TestGateRefusesToStartWithoutSignInStore(t *testing.T) {
	signInStore := newFakeSignInStore(nil)
	signInStore.Close()
	if _, err := NewSignInGate(context.Background(), signInStore.URL); err == nil {
		t.Fatal("made a gate with sign-in-store down; want an error")
	}
}

func TestCrossSiteChangesAreRefusedWith403(t *testing.T) {
	setupTestStore(t)
	handler, _ := newGatedTestServer(t)
	for _, fetchSite := range []string{"cross-site", "same-site"} {
		code := serve(handler, "POST", "/api/session/new", testAdministratorCookie, map[string]string{"Sec-Fetch-Site": fetchSite}).Code
		if code != http.StatusForbidden {
			t.Errorf("POST with Sec-Fetch-Site %s: got %d, want 403", fetchSite, code)
		}
	}
	for _, fetchSite := range []string{"same-origin", "none", ""} {
		headers := map[string]string{}
		if fetchSite != "" {
			headers["Sec-Fetch-Site"] = fetchSite
		}
		code := serve(handler, "POST", "/api/session/new", testAdministratorCookie, headers).Code
		if code != http.StatusOK {
			t.Errorf("POST with Sec-Fetch-Site %q: got %d, want 200", fetchSite, code)
		}
	}
	code := serve(handler, "GET", "/api/transcripts", testAdministratorCookie, map[string]string{"Sec-Fetch-Site": "cross-site"}).Code
	if code != http.StatusOK {
		t.Errorf("cross-site GET: got %d, want 200; reading is not refused on this header", code)
	}
}

func TestPageShellStaysPublic(t *testing.T) {
	handler, _ := newGatedTestServer(t)
	for _, path := range []string{"/", "/argraphments/", "/argraphments/convo/bold-fox"} {
		if code := serve(handler, "GET", path, "", nil).Code; code != http.StatusOK {
			t.Errorf("GET %s with no cookie: got %d, want 200", path, code)
		}
	}
}

func TestSignInAnswersAreCachedAndExpiredOnesDropped(t *testing.T) {
	setupTestStore(t)
	signInStore := newFakeSignInStore(map[string]signInSession{
		testAdministratorCookie: {PrincipalID: "principal_000001", IsAdministrator: true},
	})
	t.Cleanup(signInStore.Close)
	gate := newTestSignInGate(signInStore.URL)
	clock := time.Unix(1_800_000_000, 0)
	gate.now = func() time.Time { return clock }
	handler := newServeMux(gate)

	serve(handler, "GET", "/api/transcripts", testAdministratorCookie, nil)
	serve(handler, "GET", "/api/transcripts", testAdministratorCookie, nil)
	if checks := signInStore.sessionChecks.Load(); checks != 1 {
		t.Fatalf("two calls within the cache lifetime asked sign-in-store %d times, want 1", checks)
	}
	clock = clock.Add(signInSessionCacheLifetime)
	serve(handler, "GET", "/api/transcripts", "another-made-up-cookie", nil)
	gate.cacheMutex.Lock()
	entries := len(gate.cache)
	gate.cacheMutex.Unlock()
	if entries != 1 {
		t.Fatalf("cache holds %d entries after the first expired, want 1", entries)
	}
	for index := 0; index < signInSessionCacheMaximumEntries+10; index++ {
		gate.remember(strings.Repeat("k", index+1), nil)
	}
	if len(gate.cache) > signInSessionCacheMaximumEntries {
		t.Fatalf("cache grew to %d entries, past its bound of %d", len(gate.cache), signInSessionCacheMaximumEntries)
	}
}
