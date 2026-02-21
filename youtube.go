package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var videoIDRegex = regexp.MustCompile(`(?:v=|youtu\.be/|/embed/|/v/)([a-zA-Z0-9_-]{11})`)

func extractVideoID(rawURL string) (string, error) {
	m := videoIDRegex.FindStringSubmatch(rawURL)
	if m == nil {
		if len(rawURL) == 11 {
			return rawURL, nil
		}
		return "", fmt.Errorf("could not extract video ID from URL")
	}
	return m[1], nil
}

// fetchYouTubeTranscript uses yt-dlp to grab auto-generated captions.
type TimedSegment struct {
	StartMs int64  `json:"start_ms"`
	Text    string `json:"text"`
}

func fetchYouTubeTranscript(videoURL string) (text string, title string, segments []TimedSegment, err error) {
	videoID, err := extractVideoID(videoURL)
	if err != nil {
		return "", "", nil, err
	}

	ytdlp, err := findYtDlp()
	if err != nil {
		return "", "", nil, err
	}

	// Create temp dir for output
	tmpDir, err := os.MkdirTemp("", "yt-transcript-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "video")

	// Get title first
	titleCmd := exec.Command(ytdlp, "--skip-download", "--print", "%(title)s", videoURL)
	titleOut, _ := titleCmd.Output()
	title = strings.TrimSpace(string(titleOut))

	// Download auto-subs as json3
	args := []string{
		"--write-auto-sub",
		"--sub-lang", "en,en-orig",
		"--sub-format", "json3",
		"--skip-download",
		"-o", outPath,
	}

	// Use cookies file if available
	cookiesFile := filepath.Join(filepath.Dir(os.Args[0]), "cookies.txt")
	if _, err := os.Stat(cookiesFile); err != nil {
		// Also check working directory
		cookiesFile = "cookies.txt"
	}
	if _, err := os.Stat(cookiesFile); err == nil {
		args = append(args, "--cookies", cookiesFile)
	}

	// Ensure deno is in PATH
	denoPath := filepath.Join(os.Getenv("HOME"), ".deno", "bin")
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, denoPath) {
		os.Setenv("PATH", denoPath+":"+currentPath)
	}

	// Use impersonation if available
	args = append(args, "--impersonate", "chrome")

	args = append(args, "https://www.youtube.com/watch?v="+videoID)
	cmd := exec.Command(ytdlp, args...)

	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if strings.Contains(errMsg, "Sign in to confirm") {
			return "", title, nil, fmt.Errorf("YouTube requires authentication for this video. A cookies.txt file is needed on the server")
		}
		if strings.Contains(errMsg, "429") {
			return "", title, nil, fmt.Errorf("YouTube rate limited — try again later")
		}
		return "", title, nil, fmt.Errorf("yt-dlp failed: %s", strings.TrimSpace(errMsg))
	}

	// Find the json3 file
	matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.json3"))
	if len(matches) == 0 {
		return "", title, nil, fmt.Errorf("no subtitle file generated")
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		return "", title, nil, fmt.Errorf("failed to read subtitle file: %w", err)
	}

	// Parse json3 format — extract text with timestamps
	var captionData struct {
		Events []struct {
			TStartMs int64 `json:"tStartMs"`
			Segs     []struct {
				UTF8 string `json:"utf8"`
			} `json:"segs"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &captionData); err != nil {
		return "", title, nil, fmt.Errorf("failed to parse json3: %w", err)
	}

	segments = nil
	var sb strings.Builder
	for _, event := range captionData.Events {
		var eventText strings.Builder
		for _, seg := range event.Segs {
			eventText.WriteString(seg.UTF8)
			sb.WriteString(seg.UTF8)
		}
		t := strings.TrimSpace(eventText.String())
		if t != "" {
			segments = append(segments, TimedSegment{StartMs: event.TStartMs, Text: t})
		}
	}

	raw := sb.String()
	raw = strings.ReplaceAll(raw, "\n", " ")
	for strings.Contains(raw, "  ") {
		raw = strings.ReplaceAll(raw, "  ", " ")
	}
	raw = strings.TrimSpace(raw)

	if raw == "" {
		return "", title, nil, fmt.Errorf("captions were empty")
	}

	return raw, title, segments, nil
}

// findYtDlp locates the yt-dlp binary.
func findYtDlp() (string, error) {
	// Check PATH first
	if p, err := exec.LookPath("yt-dlp"); err == nil {
		return p, nil
	}
	// Common locations
	for _, p := range []string{
		"/usr/local/bin/yt-dlp",
		"/usr/bin/yt-dlp",
		filepath.Join(os.Getenv("HOME"), ".local/bin/yt-dlp"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("yt-dlp not found — install with: pip install yt-dlp")
}

// POST /api/import/youtube
func handleAPIImportYouTube(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		URL       string `json:"url"`
		TitleOnly bool   `json:"title_only"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		jsonError(w, "invalid request: provide url", 400)
		return
	}

	text, title, segments, err := fetchYouTubeTranscript(req.URL)
	if err != nil {
		// For title_only, try to at least return what we can
		if req.TitleOnly && title != "" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"title": title, "url": req.URL})
			return
		}
		jsonError(w, fmt.Sprintf("YouTube import failed: %v", err), 400)
		return
	}

	if req.TitleOnly {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"title": title, "url": req.URL})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"text":     text,
		"title":    title,
		"url":      req.URL,
		"segments": segments,
	})
}
