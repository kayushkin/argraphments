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
	"net/url"
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

// handleAnalyzeRaw returns incremental argument analysis as JSON
// Accepts "new_text" (new portion) and "existing" (JSON of current structure) for incremental mode
// Or just "transcript" for full analysis
func handleAnalyzeRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	newText := strings.TrimSpace(r.FormValue("new_text"))
	existingJSON := strings.TrimSpace(r.FormValue("existing"))
	fullTranscript := strings.TrimSpace(r.FormValue("transcript"))

	if newText != "" {
		// Incremental mode
		var existing []Statement
		if existingJSON != "" {
			json.Unmarshal([]byte(existingJSON), &existing)
		}

		newStatements, err := extractIncremental(newText, existing)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"mode":       "incremental",
			"statements": newStatements,
		})
	} else if fullTranscript != "" {
		// Full analysis mode
		structure, err := extractStructure(fullTranscript)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"mode":       "full",
			"statements": structure,
		})
	} else {
		http.Error(w, "no transcript", 400)
	}
}

func extractIncremental(newText string, existing []Statement) ([]Statement, error) {
	// Build a summary of existing structure for context
	existingSummary := summarizeStatements(existing, 0)

	prompt := `You are analyzing a LIVE conversation incrementally. You've already analyzed earlier parts of the conversation.

EXISTING ANALYSIS (for context only — do NOT repeat these):
` + existingSummary + `

NEW PORTION OF CONVERSATION to analyze:
` + newText + `

Return ONLY new statements from the new portion. Each statement has:
- "speaker": who said it (use same speaker labels as existing analysis)
- "text": the core claim or statement (paraphrased concisely)
- "type": one of "claim", "response", "question", "agreement", "rebuttal", "tangent", "clarification", "evidence"
- "children": array of sub-statements (only if there are direct responses within the new text)
- "parent_text": if this new statement is a direct response to an EXISTING statement, include the text of that existing statement here (so we can nest it). Omit if it's a new top-level statement.
- "fact_check": ONLY if the statement contains an objectively false/misleading factual claim. Object with "verdict" (false/misleading/unverified/mostly-true), "correction", "search_query".

Rules:
- Only return statements from the NEW text
- Do NOT regenerate or repeat existing statements
- If the new text is just continuation of the same topic with no new claims, return an empty array
- Keep speaker labels consistent with existing analysis
- Only flag objective factual claims, not opinions

Return ONLY valid JSON array, no markdown fences.`

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
		return nil, fmt.Errorf("empty response")
	}

	text := strings.TrimSpace(result.Content[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	// Parse — these may have an extra "parent_text" field
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse error: %v", err)
	}

	var statements []Statement
	for _, r := range raw {
		var s struct {
			Statement
			ParentText string `json:"parent_text"`
		}
		json.Unmarshal(r, &s)
		// ParentText is handled client-side for nesting
		s.Statement.Type = strings.ToLower(s.Statement.Type)
		statements = append(statements, s.Statement)
	}

	return statements, nil
}

func summarizeStatements(statements []Statement, depth int) string {
	if len(statements) == 0 {
		return "(none yet)"
	}
	var sb strings.Builder
	for _, s := range statements {
		indent := strings.Repeat("  ", depth)
		sb.WriteString(fmt.Sprintf("%s- [%s] %s: %s\n", indent, s.Type, s.Speaker, s.Text))
		if len(s.Children) > 0 {
			sb.WriteString(summarizeStatements(s.Children, depth+1))
		}
	}
	return sb.String()
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
- If it's a monologue, use a single speaker
- Keep the original wording, don't paraphrase
- Split at natural speaker boundaries

NAME DETECTION (important):
- When someone says a name, they are almost always addressing the OTHER person, not themselves
- "Hey John, what do you think?" → the LISTENER is John, not the speaker
- "Thanks Sarah" → Sarah is the person being thanked, not the one speaking
- "I'm Mike" or "My name is Mike" → rare case where they ARE naming themselves
- Apply this logic carefully to assign detected names to the correct speaker

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

Use speaker IDs like "speaker_1", "speaker_2", etc. Put detected names in the speakers map for the correct person (the one being addressed, not the one speaking). Leave as empty string if no name detected.

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
	Speaker   string      `json:"speaker"`
	Text      string      `json:"text"`
	Type      string      `json:"type"` // claim, response, question, agreement, rebuttal, tangent
	Children  []Statement `json:"children"`
	FactCheck *FactCheck  `json:"fact_check,omitempty"`
}

type FactCheck struct {
	Verdict     string `json:"verdict"`      // "false", "misleading", "unverified", "mostly-true"
	Correction  string `json:"correction"`   // what's actually true
	SearchQuery string `json:"search_query"` // google search query to verify
}

func extractStructure(transcript string) ([]Statement, error) {
	prompt := `Analyze this conversation transcript and extract a nested argument/discussion structure.

IMPORTANT — Speaker identification:
The transcript may already have speaker labels (e.g. "Speaker 1: ..." or names). If so, use them directly.
If not, identify distinct speakers from context clues: opinion shifts, turn-taking, different perspectives.
Be consistent — the same person should always get the same label.

Return a JSON array of top-level statements. Each statement has:
- "speaker": who said it
- "text": the core claim or statement (paraphrased concisely)
- "type": one of "claim", "response", "question", "agreement", "rebuttal", "tangent", "clarification", "evidence"
- "children": array of statements that are direct responses/follow-ups to this one
- "fact_check": ONLY include this field if the statement contains a factual claim that is false, misleading, or dubiously inaccurate based on your knowledge. Object with:
  - "verdict": one of "false", "misleading", "unverified", "mostly-true"
  - "correction": brief explanation of what's actually true
  - "search_query": a Google search query the user can use to verify

Nest responses under the statement they're responding to. A rebuttal to a claim goes as a child of that claim.

FACT-CHECKING RULES:
- Only flag objective factual claims, NOT opinions or subjective statements
- "I think X is better" = opinion, don't flag
- "X was invented in 1990" = factual, flag if wrong
- Be conservative — only flag things you're confident about
- Include fact_check field ONLY on flagged statements, omit it otherwise

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

	flagged := ""
	if s.FactCheck != nil {
		flagged = " flagged flagged-" + template.HTMLEscapeString(s.FactCheck.Verdict)
	}

	sb.WriteString(fmt.Sprintf(`<div class="statement depth-%d type-%s%s"`, depth, typeClass, flagged))
	sb.WriteString(` >`)

	factCheckHTML := renderFactCheck(s.FactCheck)

	if hasChildren {
		sb.WriteString(`<details>`)
		sb.WriteString(fmt.Sprintf(`<summary><span class="type-badge">%s</span> <span class="speaker">%s:</span> %s <span class="child-count">(%d)</span>%s</summary>`, typeClass, speaker, text, countDescendants(s), factCheckHTML))
		sb.WriteString(`<div class="children">`)
		for _, child := range s.Children {
			renderStatement(sb, child, depth+1)
		}
		sb.WriteString(`</div></details>`)
	} else {
		sb.WriteString(fmt.Sprintf(`<div class="leaf"><span class="type-badge">%s</span> <span class="speaker">%s:</span> %s%s</div>`, typeClass, speaker, text, factCheckHTML))
	}

	sb.WriteString(`</div>`)
}

func renderFactCheck(fc *FactCheck) string {
	if fc == nil {
		return ""
	}
	verdict := template.HTMLEscapeString(fc.Verdict)
	correction := template.HTMLEscapeString(fc.Correction)
	searchURL := "https://www.google.com/search?q=" + url.QueryEscape(fc.SearchQuery)

	return fmt.Sprintf(`<div class="fact-check verdict-%s">
		<span class="fact-verdict">⚠ %s</span>
		<span class="fact-correction">%s</span>
		<a href="%s" target="_blank" rel="noopener" class="fact-source">verify ↗</a>
	</div>`, verdict, verdict, correction, searchURL)
}

func countDescendants(s Statement) int {
	count := len(s.Children)
	for _, c := range s.Children {
		count += countDescendants(c)
	}
	return count
}
