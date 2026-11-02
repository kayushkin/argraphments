package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kayushkin/llm-bridge/servicesettings"
)

func fakeAuthStore(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer store-token" || r.Header.Get("X-Auth-App") != "argraphments" || r.Header.Get("X-Auth-Reason") == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/credentials/cred_0a1b2c3d4e5f60718293a4b5/resolve":
			w.Write([]byte(`{"id":"cred_0a1b2c3d4e5f60718293a4b5","api_key":"sk-test"}`))
		case "/api/credentials/cred_000000000000000000000000/resolve":
			w.Write([]byte(`{"id":"cred_000000000000000000000000","api_key":""}`))
		default:
			http.Error(w, "credential not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestResolveAPIKeyReadsTheKeyByCredentialID(t *testing.T) {
	store := fakeAuthStore(t)
	key, err := resolveAPIKey(store.URL+"/", "store-token", "cred_0a1b2c3d4e5f60718293a4b5", "argraphments:claude")
	if err != nil || key != "sk-test" {
		t.Fatalf("key %q, error %v; want sk-test", key, err)
	}
}

func TestResolveAPIKeyFailsLoudly(t *testing.T) {
	store := fakeAuthStore(t)
	for name, c := range map[string]struct{ token, id, want string }{
		"unknown credential": {"store-token", "cred_ffffffffffffffffffffffff", "404"},
		"wrong token":        {"other-token", "cred_0a1b2c3d4e5f60718293a4b5", "401"},
		"no api_key":         {"store-token", "cred_000000000000000000000000", "has no api_key"},
	} {
		if _, err := resolveAPIKey(store.URL, c.token, c.id, "argraphments:claude"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v; want one mentioning %q", name, err, c.want)
		}
	}
}

func TestSettingsRefuseToStartWithoutTheCredentialsOrWithAnUndeclaredVariable(t *testing.T) {
	complete := map[string]string{
		"ARGRAPHMENTS_AUTH_STORE_URL":          "http://127.0.0.1:1",
		"ARGRAPHMENTS_AUTH_STORE_TOKEN":        "store-token",
		"ARGRAPHMENTS_ANTHROPIC_CREDENTIAL_ID": "cred_0a1b2c3d4e5f60718293a4b5",
		"ARGRAPHMENTS_OPENAI_CREDENTIAL_ID":    "cred_0a1b2c3d4e5f60718293a4b6",
	}
	registry, err := NewSettingsRegistry(servicesettings.MapEnvironment(complete))
	if err != nil {
		t.Fatalf("complete settings: %v", err)
	}
	if got := registry.String(SettingListenAddress); got != "127.0.0.1:8086" {
		t.Errorf("listen address %q; want the loopback default", got)
	}
	missing := map[string]string{}
	for k, v := range complete {
		if k != "ARGRAPHMENTS_OPENAI_CREDENTIAL_ID" {
			missing[k] = v
		}
	}
	if _, err := NewSettingsRegistry(servicesettings.MapEnvironment(missing)); err == nil {
		t.Error("started without the OpenAI credential id")
	}
	undeclared := map[string]string{"ARGRAPHMENTS_OPENAI_API_KEY": "sk-test"}
	for k, v := range complete {
		undeclared[k] = v
	}
	if _, err := NewSettingsRegistry(servicesettings.MapEnvironment(undeclared)); err == nil {
		t.Error("started with an undeclared ARGRAPHMENTS_ variable")
	}
}
