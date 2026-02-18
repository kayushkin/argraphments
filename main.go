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

	var err error
	templates, err = loadTemplates()
	if err != nil {
		log.Fatal(err)
	}

	os.MkdirAll("uploads", 0755)

	prefix := getEnv("BASE_PATH", "/argraphments")

	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/", handleIndex)
	mux.HandleFunc(prefix+"/transcribe", handleTranscribe)
	mux.HandleFunc(prefix+"/analyze", handleAnalyze)
	mux.HandleFunc(prefix+"/transcribe-raw", handleTranscribeRaw)
	mux.HandleFunc(prefix+"/diarize", handleDiarize)
	mux.HandleFunc(prefix+"/analyze-raw", handleAnalyzeRaw)
	mux.Handle(prefix+"/static/", http.StripPrefix(prefix+"/static/", http.FileServer(http.Dir("static"))))

	port := getEnv("PORT", "8086")
	addr := "127.0.0.1:" + port
	log.Printf("argraphments listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func loadTemplates() (*template.Template, error) {
	return template.ParseGlob("templates/*.html")
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

// handleTranscribeRaw returns plain text transcript (for live chunked transcription)
func handleTranscribeRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseMultipartForm(50 << 20)

	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "no audio file", 400)
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = ".webm"
	}
	tmpPath := fmt.Sprintf("uploads/%d%s", time.Now().UnixNano(), ext)
	dst, err := os.Create(tmpPath)
	if err != nil {
		http.Error(w, "failed to save", 500)
		return
	}
	io.Copy(dst, file)
	dst.Close()
	defer os.Remove(tmpPath)

	transcript, err := whisperTranscribe(tmpPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, transcript)
}

// handleAnalyzeRaw returns just the argument tree HTML fragment
func handleAnalyzeRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transcript := r.FormValue("transcript")
	if strings.TrimSpace(transcript) == "" {
		http.Error(w, "no transcript", 400)
		return
	}

	structure, err := extractStructure(transcript)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, renderStructureRaw(structure))
}

func renderStructureRaw(statements []Statement) string {
	var sb strings.Builder
	sb.WriteString(`<div class="argument-tree">`)
	for _, s := range statements {
		renderStatement(&sb, s, 0)
	}
	sb.WriteString(`</div>`)
	return sb.String()
}

// handleDiarize takes raw transcript, returns speaker-labeled JSON
func handleDiarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	transcript := strings.TrimSpace(r.FormValue("transcript"))
	if transcript == "" {
		http.Error(w, "no transcript", 400)
		return
	}

	result, err := diarizeTranscript(transcript)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

type DiarizeResult struct {
	Speakers map[string]string `json:"speakers"` // id -> detected name
	Messages []DiarizeMessage  `json:"messages"`
}

type DiarizeMessage struct {
	Speaker string `json:"speaker"` // speaker id
	Text    string `json:"text"`
}

func diarizeTranscript(transcript string) (*DiarizeResult, error) {
	prompt := `You are a conversation diarization system. Given a raw transcript (which may have no speaker labels), identify distinct speakers and split the text into a conversation.

Rules:
- Identify speaker changes from context: opinion shifts, turn-taking, Q&A patterns, different perspectives
- If speakers mention their names or are addressed by name, capture those names
- If it's a monologue, use a single speaker
- Keep the original wording, don't paraphrase
- Split at natural speaker boundaries

Return JSON with this exact structure:
{
  "speakers": {
    "speaker_1": "detected name or empty string",
    "speaker_2": "detected name or empty string"
  },
  "messages": [
    {"speaker": "speaker_1", "text": "what they said"},
    {"speaker": "speaker_2", "text": "what they said"}
  ]
}

Use speaker IDs like "speaker_1", "speaker_2", etc. If you detect a real name from the conversation (e.g. someone says "Thanks John" or "I'm Sarah"), put that name in the speakers map. Otherwise leave it as an empty string.

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

	var apiResult struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &apiResult); err != nil {
		return nil, err
	}
	if len(apiResult.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	text := strings.TrimSpace(apiResult.Content[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result DiarizeResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("parse error: %v\nraw: %s", err, text)
	}
	return &result, nil
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

IMPORTANT — Speaker identification:
The transcript may not have speaker labels. You MUST identify distinct speakers from context clues:
- Changes in position/opinion (one person argues for X, another against)
- Turn-taking patterns, conversational flow
- Different speaking styles, vocabulary, or perspectives
- Phrases like "I disagree", "but", "well actually" often signal a speaker change
- Questions followed by answers typically involve different speakers
Label them as "Speaker 1", "Speaker 2", etc. Be consistent — the same person should always get the same label. If it's clearly a monologue, use a single speaker.

Return a JSON array of top-level statements. Each statement has:
- "speaker": who said it (use "Speaker 1", "Speaker 2" etc)
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
	sb.WriteString(`<div class="action-row"><button class="btn btn-secondary" onclick="showInputs()">New</button></div>`)
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
