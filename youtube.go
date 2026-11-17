package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var videoIDRegex = regexp.MustCompile(`(?:v=|youtu\.be/|/embed/|/v/)([a-zA-Z0-9_-]{11})`)

var bareVideoIDRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{11}$`)

func extractVideoID(rawURL string) (string, error) {
	m := videoIDRegex.FindStringSubmatch(rawURL)
	if m == nil {
		if bareVideoIDRegex.MatchString(rawURL) {
			return rawURL, nil
		}
		return "", fmt.Errorf("could not extract video ID from URL")
	}
	return m[1], nil
}

// canonicalYouTubeURL is the only address yt-dlp is ever given: built from
// the parsed video id, never the visitor's own string.
func canonicalYouTubeURL(videoID string) string {
	return "https://www.youtube.com/watch?v=" + videoID
}

// youTubeTitleArguments are yt-dlp's arguments to print a video's title. The
// address follows "--", so yt-dlp reads it as an address and never an option.
func youTubeTitleArguments(videoID string) []string {
	return []string{"--skip-download", "--print", "%(title)s", "--", canonicalYouTubeURL(videoID)}
}

// youTubeCaptionArguments are yt-dlp's arguments to write a video's English
// captions as json3 to outputPath. cookiesFile may be empty.
func youTubeCaptionArguments(videoID, outputPath, cookiesFile string) []string {
	arguments := []string{
		"--write-auto-sub",
		"--sub-lang", "en,en-orig",
		"--sub-format", "json3",
		"--skip-download",
		"-o", outputPath,
	}
	if cookiesFile != "" {
		arguments = append(arguments, "--cookies", cookiesFile)
	}
	arguments = append(arguments, "--impersonate", "chrome")
	return append(arguments, "--", canonicalYouTubeURL(videoID))
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

	// Get title first. A failure here is logged; the caption call below
	// reports its own error.
	titleOut, err := exec.Command(ytdlp, youTubeTitleArguments(videoID)...).Output()
	if err != nil {
		log.Printf("yt-dlp could not read the title of %s: %v", videoID, err)
	}
	title = strings.TrimSpace(string(titleOut))

	// Use cookies file if available
	cookiesFile := filepath.Join(filepath.Dir(os.Args[0]), "cookies.txt")
	if _, err := os.Stat(cookiesFile); err != nil {
		// Also check working directory
		cookiesFile = "cookies.txt"
	}
	if _, err := os.Stat(cookiesFile); err != nil {
		cookiesFile = ""
	}

	// Ensure deno is in PATH
	denoPath := filepath.Join(os.Getenv("HOME"), ".deno", "bin")
	currentPath := os.Getenv("PATH")
	if !strings.Contains(currentPath, denoPath) {
		os.Setenv("PATH", denoPath+":"+currentPath)
	}

	args := youTubeCaptionArguments(videoID, outPath, cookiesFile)
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
