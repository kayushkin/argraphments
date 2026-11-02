package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var authStoreHTTPClient = &http.Client{Timeout: 15 * time.Second}

// resolveAPIKey reads the api_key of one auth-store credential by its id.
func resolveAPIKey(authStoreURL, authStoreToken, credentialID, reason string) (string, error) {
	request, err := http.NewRequest(http.MethodGet, strings.TrimRight(authStoreURL, "/")+"/api/credentials/"+credentialID+"/resolve", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+authStoreToken)
	request.Header.Set("X-Auth-App", ServiceName)
	request.Header.Set("X-Auth-Reason", reason)
	response, err := authStoreHTTPClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth-store answered %d for credential %s: %s", response.StatusCode, credentialID, strings.TrimSpace(string(body)))
	}
	var credential struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(body, &credential); err != nil {
		return "", fmt.Errorf("auth-store's answer for credential %s does not parse: %w", credentialID, err)
	}
	if credential.APIKey == "" {
		return "", errors.New("auth-store credential " + credentialID + " has no api_key")
	}
	return credential.APIKey, nil
}
