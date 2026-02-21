package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractVideoID(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://www.youtube.com/watch?v=aSMoF10iD-g", "aSMoF10iD-g"},
		{"https://youtu.be/aSMoF10iD-g", "aSMoF10iD-g"},
		{"https://www.youtube.com/embed/aSMoF10iD-g", "aSMoF10iD-g"},
		{"aSMoF10iD-g", "aSMoF10iD-g"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ&t=10s", "dQw4w9WgXcQ"},
	}
	for _, c := range cases {
		got, err := extractVideoID(c.input)
		if err != nil {
			t.Errorf("extractVideoID(%q): %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("extractVideoID(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestExtractVideoID_Invalid(t *testing.T) {
	_, err := extractVideoID("not-a-url")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestYouTubeImportAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live YouTube test in short mode")
	}

	// Check yt-dlp is available
	if _, err := findYtDlp(); err != nil {
		t.Skip("yt-dlp not installed:", err)
	}

	setupTestStore(t)
	mux := setupMux()

	body := `{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`
	req := httptest.NewRequest("POST", "/api/import/youtube", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		if strings.Contains(w.Body.String(), "429") {
			t.Skip("rate limited by YouTube, skipping")
		}
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	json.Unmarshal(w.Body.Bytes(), &result)

	if result["title"] == "" {
		t.Error("expected title")
	}
	if len(result["text"]) < 100 {
		t.Fatalf("expected substantial transcript, got %d chars", len(result["text"]))
	}
	t.Logf("Title: %s, %d chars", result["title"], len(result["text"]))
}

func TestYouTubeImportAPI_InvalidURL(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	body := `{"url":"not-a-url"}`
	req := httptest.NewRequest("POST", "/api/import/youtube", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400 for invalid URL, got %d", w.Code)
	}
}

func TestYouTubeImportAPI_NoBody(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	req := httptest.NewRequest("POST", "/api/import/youtube", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
