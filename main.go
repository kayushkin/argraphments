package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	anthropicKey string
	openaiKey    string
	templates    *template.Template
)

func main() {
	anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	openaiKey = os.Getenv("OPENAI_API_KEY")
	if anthropicKey == "" {
		log.Fatal("ANTHROPIC_API_KEY required")
	}
	if openaiKey == "" {
		log.Fatal("OPENAI_API_KEY required")
	}

	templates = template.Must(template.ParseGlob("templates/*.html"))

	os.MkdirAll("uploads", 0755)

	prefix := getEnv("BASE_PATH", "/argraphments")

	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/", handleIndex)
	mux.HandleFunc(prefix+"/transcribe", handleTranscribe)
	mux.HandleFunc(prefix+"/analyze", handleAnalyze)
	mux.HandleFunc(prefix+"/youtube", handleYouTube)
	mux.Handle(prefix+"/static/", http.StripPrefix(prefix+"/static/", http.FileServer(http.Dir("static"))))

	port := getEnv("PORT", "8086")
	addr := "127.0.0.1:" + port
	log.Printf("argraphments listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	templates.ExecuteTemplate(w, "index.html", nil)
}

// handleTranscribe receives audio, sends to Whisper, returns transcript HTML
func handleTranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseMultipartForm(50 << 20) // 50MB max

	file, header, err := r.FormFile("audio")
	if err != nil {
		htmxError(w, "No audio file provided")
		return
	}
	defer file.Close()

	// Save temporarily
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".webm"
	}
	tmpPath := fmt.Sprintf("uploads/%d%s", time.Now().UnixNano(), ext)
	dst, err := os.Create(tmpPath)
	if err != nil {
		htmxError(w, "Failed to save upload")
		return
	}
	io.Copy(dst, file)
	dst.Close()
	defer os.Remove(tmpPath)

	// Transcribe via Whisper
	transcript, err := whisperTranscribe(tmpPath)
	if err != nil {
		htmxError(w, fmt.Sprintf("Transcription failed: %v", err))
		return
	}

	// Return transcript in editable form + analyze button
	w.Header().Set("Content-Type", "text/html")
	templates.ExecuteTemplate(w, "transcript.html", map[string]string{
		"Transcript": transcript,
	})
}

// handleAnalyze takes transcript text, extracts argument structure via Claude
func handleAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transcript := r.FormValue("transcript")
	if strings.TrimSpace(transcript) == "" {
		htmxError(w, "No transcript provided")
		return
	}

	structure, err := extractStructure(transcript)
	if err != nil {
		htmxError(w, fmt.Sprintf("Analysis failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, renderStructure(structure))
}

// handleYouTube fetches transcript from a YouTube video
func handleYouTube(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	url := strings.TrimSpace(r.FormValue("url"))
	if url == "" {
		htmxError(w, "No URL provided")
		return
	}

	// Try fetching existing subtitles first
	transcript, err := ytSubtitles(url)
	if err != nil {
		// Fall back: download audio and transcribe via Whisper
		transcript, err = ytAudioTranscribe(url)
		if err != nil {
			htmxError(w, fmt.Sprintf("YouTube failed: %v", err))
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	templates.ExecuteTemplate(w, "transcript.html", map[string]string{
		"Transcript": transcript,
	})
}

// ytSubtitles tries to get existing captions via yt-dlp
func ytSubtitles(url string) (string, error) {
	tmpDir := fmt.Sprintf("uploads/yt-%d", time.Now().UnixNano())
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "subs")

	cmd := exec.Command("yt-dlp",
		"--write-subs", "--write-auto-subs",
		"--sub-lang", "en",
		"--sub-format", "vtt",
		"--skip-download",
		"-o", outPath,
		url,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("yt-dlp subs: %v: %s", err, string(out))
	}

	// Find the subtitle file
	matches, _ := filepath.Glob(tmpDir + "/*.vtt")
	if len(matches) == 0 {
		return "", fmt.Errorf("no subtitles found")
	}

	raw, err := os.ReadFile(matches[0])
	if err != nil {
		return "", err
	}

	return cleanVTT(string(raw)), nil
}

// ytAudioTranscribe downloads audio and sends to Whisper
func ytAudioTranscribe(url string) (string, error) {
	tmpDir := fmt.Sprintf("uploads/yt-%d", time.Now().UnixNano())
	os.MkdirAll(tmpDir, 0755)
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "audio.mp3")

	cmd := exec.Command("yt-dlp",
		"-x", "--audio-format", "mp3",
		"--audio-quality", "5",
		"-o", outPath,
		url,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("yt-dlp audio: %v: %s", err, string(out))
	}

	// yt-dlp might add extension
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		matches, _ := filepath.Glob(tmpDir + "/audio.*")
		if len(matches) == 0 {
			return "", fmt.Errorf("no audio file produced")
		}
		outPath = matches[0]
	}

	return whisperTranscribe(outPath)
}

// cleanVTT strips VTT timestamps and deduplicates lines
func cleanVTT(raw string) string {
	lines := strings.Split(raw, "\n")
	var result []string
	seen := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip headers, timestamps, empty lines
		if line == "" || line == "WEBVTT" || strings.Contains(line, "-->") || strings.HasPrefix(line, "Kind:") || strings.HasPrefix(line, "Language:") || strings.HasPrefix(line, "NOTE") {
			continue
		}
		// Strip VTT tags like <c> </c> <00:00:01.234>
		clean := stripVTTTags(line)
		clean = strings.TrimSpace(clean)
		if clean == "" || clean == seen {
			continue
		}
		seen = clean
		result = append(result, clean)
	}
	return strings.Join(result, " ")
}

func stripVTTTags(s string) string {
	// Remove <...> tags
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func htmxError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<div class="error">%s</div>`, template.HTMLEscapeString(msg))
}

// --- Whisper API ---

func whisperTranscribe(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	io.Copy(part, f)

	writer.WriteField("model", "whisper-1")
	writer.WriteField("response_format", "text")
	writer.Close()

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+openaiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("whisper API %d: %s", resp.StatusCode, string(body))
	}

	return strings.TrimSpace(string(body)), nil
}

// --- Claude API for structure extraction ---

type Statement struct {
	Speaker  string      `json:"speaker"`
	Text     string      `json:"text"`
	Type     string      `json:"type"` // claim, response, question, agreement, rebuttal, tangent
	Children []Statement `json:"children"`
}

func extractStructure(transcript string) ([]Statement, error) {
	prompt := `Analyze this conversation transcript and extract a nested argument/discussion structure.

Return a JSON array of top-level statements. Each statement has:
- "speaker": who said it (use "Speaker 1", "Speaker 2" etc if unknown)
- "text": the core claim or statement (paraphrased concisely)
- "type": one of "claim", "response", "question", "agreement", "rebuttal", "tangent", "clarification", "evidence"
- "children": array of statements that are direct responses/follow-ups to this one

Nest responses under the statement they're responding to. A rebuttal to a claim goes as a child of that claim. Agreement goes as a child. Follow-up questions go as children of what they're asking about.

The goal is to show the STRUCTURE of the conversation — how ideas branch and relate — not just a linear transcript.

Return ONLY valid JSON, no markdown fences.

Transcript:
` + transcript

	reqBody, _ := json.Marshal(map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 4096,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", anthropicKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("claude API %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response from Claude")
	}

	var statements []Statement
	text := strings.TrimSpace(result.Content[0].Text)
	// Strip markdown fences if Claude adds them anyway
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	if err := json.Unmarshal([]byte(text), &statements); err != nil {
		return nil, fmt.Errorf("failed to parse structure: %v\nraw: %s", err, text)
	}

	return statements, nil
}

func renderStructure(statements []Statement) string {
	var sb strings.Builder
	sb.WriteString(`<div class="argument-tree">`)
	for _, s := range statements {
		renderStatement(&sb, s, 0)
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

func renderStatement(sb *strings.Builder, s Statement, depth int) {
	hasChildren := len(s.Children) > 0
	typeClass := template.HTMLEscapeString(s.Type)
	speaker := template.HTMLEscapeString(s.Speaker)
	text := template.HTMLEscapeString(s.Text)

	sb.WriteString(fmt.Sprintf(`<div class="statement depth-%d type-%s"`, depth, typeClass))
	sb.WriteString(` >`)

	if hasChildren {
		sb.WriteString(`<details>`)
		sb.WriteString(fmt.Sprintf(`<summary><span class="type-badge">%s</span> <span class="speaker">%s:</span> %s <span class="child-count">(%d)</span></summary>`, typeClass, speaker, text, countDescendants(s)))
		sb.WriteString(`<div class="children">`)
		for _, child := range s.Children {
			renderStatement(sb, child, depth+1)
		}
		sb.WriteString(`</div></details>`)
	} else {
		sb.WriteString(fmt.Sprintf(`<div class="leaf"><span class="type-badge">%s</span> <span class="speaker">%s:</span> %s</div>`, typeClass, speaker, text))
	}

	sb.WriteString(`</div>`)
}

func countDescendants(s Statement) int {
	count := len(s.Children)
	for _, c := range s.Children {
		count += countDescendants(c)
	}
	return count
}
