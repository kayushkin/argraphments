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
	"strconv"
	"strings"
	"time"

	"github.com/kayushkin/argraphments/storage"
)

var (
	anthropicKey string
	openaiKey    string
	templates    *template.Template
	store        *storage.Store
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

	dbPath := getEnv("ARGRAPHMENTS_DB", "./argraphments.db")
	store, err = storage.NewStore(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer store.Close()

	mux := http.NewServeMux()
	staticFS := http.FileServer(http.Dir("static"))
	distAssetsFS := http.FileServer(http.Dir("static/dist"))

	// Register routes on both /argraphments and / prefixes
	for _, prefix := range []string{"/argraphments", ""} {
		p := prefix
		mux.HandleFunc(p+"/", handleIndex)
		mux.HandleFunc(p+"/convo/", handleIndex) // conversation URLs
		mux.Handle(p+"/static/", http.StripPrefix(p+"/static/", staticFS))
		mux.Handle(p+"/assets/", http.StripPrefix(p, distAssetsFS))
		mux.HandleFunc(p+"/api/transcribe", handleAPITranscribe)
		mux.HandleFunc(p+"/api/diarize", handleAPIDiarize)
		mux.HandleFunc(p+"/api/analyze", handleAPIAnalyze)
		mux.HandleFunc(p+"/api/analyze-incremental", handleAPIAnalyzeIncremental)
		mux.HandleFunc(p+"/api/session/new", handleAPINewSession)
		mux.HandleFunc(p+"/api/transcripts", handleAPITranscripts)
		mux.HandleFunc(p+"/api/transcripts/", handleAPITranscripts)
		mux.HandleFunc(p+"/api/claims/", handleAPIClaim)
		mux.HandleFunc(p+"/api/speakers", handleAPISpeakers)
		mux.HandleFunc(p+"/api/speakers/", handleAPISpeakers)
		mux.HandleFunc(p+"/api/import/youtube", handleAPIImportYouTube)
		mux.HandleFunc(p+"/api/graph", handleAPIGraph)
		mux.HandleFunc(p+"/api/sample", handleAPISample)
	}

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
	// Serve React SPA, rewriting asset paths for /argraphments prefix
	data, err := os.ReadFile("static/dist/index.html")
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	html := string(data)
	// If served under /argraphments, rewrite asset paths
	if strings.HasPrefix(r.URL.Path, "/argraphments") {
		html = strings.ReplaceAll(html, `src="/assets/`, `src="/argraphments/assets/`)
		html = strings.ReplaceAll(html, `href="/assets/`, `href="/argraphments/assets/`)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// --- JSON API handlers ---

// POST /api/session/new — creates a placeholder transcript, returns slug
func handleAPINewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := store.SaveTranscript("", "")
	if err != nil {
		jsonError(w, "failed to create session", 500)
		return
	}
	t, err := store.GetTranscript(id)
	if err != nil {
		jsonError(w, "failed to get session", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"slug": t.Slug,
		"id":   t.ID,
	})
}

// POST /api/transcribe — accepts audio file, returns {"text": "..."}
func handleAPITranscribe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	r.ParseMultipartForm(50 << 20)

	file, header, err := r.FormFile("audio")
	if err != nil {
		jsonError(w, "no audio file", 400)
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
		jsonError(w, "failed to save", 500)
		return
	}
	n, _ := io.Copy(dst, file)
	dst.Close()
	defer os.Remove(tmpPath)

	if n == 0 {
		jsonError(w, "empty audio file", 400)
		return
	}

	log.Printf("Transcribe: saved %d bytes to %s", n, tmpPath)

	transcript, err := whisperTranscribe(tmpPath)
	if err != nil {
		jsonError(w, fmt.Sprintf("transcription failed: %v", err), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"text": transcript})
}

// POST /api/diarize — accepts {"transcript": "..."}, returns diarize result
func handleAPIDiarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Accept both JSON body and form data
	var transcript string
	var segments []TimedSegment
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		var req struct {
			Transcript string         `json:"transcript"`
			Segments   []TimedSegment `json:"segments,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON", 400)
			return
		}
		transcript = req.Transcript
		segments = req.Segments
	} else {
		r.ParseMultipartForm(10 << 20)
		transcript = strings.TrimSpace(r.FormValue("transcript"))
	}

	if strings.TrimSpace(transcript) == "" {
		jsonError(w, "no transcript", 400)
		return
	}

	result, err := diarizeTranscript(transcript)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Assign timestamps from segments if available
	if len(segments) > 0 && len(result.Messages) > 0 {
		assignTimestamps(result.Messages, segments)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// POST /api/analyze — full analysis, returns {"statements": [...], "transcript_id": N}
func handleAPIAnalyze(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Transcript     string                    `json:"transcript"`
		Slug           string                    `json:"slug,omitempty"`
		Speakers       map[string]string         `json:"speakers,omitempty"`
		Messages       []storage.DiarizeMessage  `json:"messages,omitempty"`
		SpeakerAutoGen map[string]bool           `json:"speaker_auto_gen,omitempty"`
		SourceURL      string                    `json:"source_url,omitempty"`
	}

	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON", 400)
			return
		}
	} else {
		r.ParseMultipartForm(10 << 20)
		req.Transcript = strings.TrimSpace(r.FormValue("transcript"))
		// Parse optional speakers/messages from form
		if s := r.FormValue("speakers"); s != "" {
			json.Unmarshal([]byte(s), &req.Speakers)
		}
		if m := r.FormValue("messages"); m != "" {
			json.Unmarshal([]byte(m), &req.Messages)
		}
	}

	if strings.TrimSpace(req.Transcript) == "" {
		jsonError(w, "no transcript", 400)
		return
	}

	analysis, err := extractStructure(req.Transcript)
	if err != nil {
		jsonError(w, fmt.Sprintf("analysis failed: %v", err), 500)
		return
	}

	var existingID int64
	if req.Slug != "" {
		if t, err := store.GetTranscriptBySlug(req.Slug); err == nil {
			existingID = t.ID
		}
	}
	tid := persistStatements("", analysis.Statements, req.Speakers, req.Messages, req.SpeakerAutoGen, existingID)

	// Update title if Claude generated one
	if analysis.Title != "" && tid > 0 {
		store.UpdateTitle(tid, analysis.Title)
	}
	// Save source URL if provided
	if req.SourceURL != "" && tid > 0 {
		store.SetSourceURL(tid, req.SourceURL)
	}

	// Get slug for response
	slug := req.Slug
	if slug == "" && tid > 0 {
		if t, err := store.GetTranscript(tid); err == nil {
			slug = t.Slug
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"statements":    analysis.Statements,
		"transcript_id": tid,
		"slug":          slug,
		"title":         analysis.Title,
	})
}

// POST /api/analyze-incremental — incremental analysis
func handleAPIAnalyzeIncremental(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		NewText     string      `json:"new_text"`
		ContextText string      `json:"context_text"`
		Existing    []Statement `json:"existing"`
		MsgOffset   int         `json:"msg_offset"`
		FullReview  bool        `json:"full_review"`
	}

	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "invalid JSON", 400)
			return
		}
	} else {
		r.ParseMultipartForm(10 << 20)
		req.NewText = strings.TrimSpace(r.FormValue("new_text"))
		if e := r.FormValue("existing"); e != "" {
			json.Unmarshal([]byte(e), &req.Existing)
		}
	}

	if strings.TrimSpace(req.NewText) == "" {
		jsonError(w, "no new_text", 400)
		return
	}

	result, err := extractIncremental(req.NewText, req.ContextText, req.Existing, req.MsgOffset, req.FullReview)
	if err != nil {
		jsonError(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// GET /api/transcripts and GET /api/transcripts/{id}
func handleAPITranscripts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	path = strings.TrimPrefix(path, "/argraphments")
	path = strings.TrimPrefix(path, "/api/transcripts")
	path = strings.TrimPrefix(path, "/")

	if path != "" {
		// Check for sub-resource: {slug}/speakers
		slug := path
		subResource := ""
		if idx := strings.Index(path, "/"); idx != -1 {
			slug = path[:idx]
			subResource = path[idx+1:]
		}

		// Try numeric ID first, then slug
		var t *storage.Transcript
		var err error
		id, parseErr := strconv.ParseInt(slug, 10, 64)
		if parseErr == nil {
			t, err = store.GetTranscript(id)
		} else {
			t, err = store.GetTranscriptBySlug(slug)
		}
		if err != nil {
			http.Error(w, `{"error":"not found"}`, 404)
			return
		}

		// Handle PUT /api/transcripts/{slug}/speakers
		if subResource == "speakers" && r.Method == http.MethodPut {
			var req struct {
				Speakers       map[string]string `json:"speakers"`
				SpeakerAutoGen map[string]bool   `json:"speaker_auto_gen,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Speakers == nil {
				jsonError(w, "invalid request", 400)
				return
			}
			// Update speakers in utterances
			_, messages, _ := store.GetDiarization(t.ID)
			if messages == nil {
				messages = []storage.DiarizeMessage{}
			}
			store.SaveDiarization(t.ID, req.Speakers, messages)
			if req.SpeakerAutoGen != nil {
				store.SaveSpeakersWithFlags(t.ID, req.Speakers, req.SpeakerAutoGen)
			}
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		// Get diarization data
		speakers, messages, _ := store.GetDiarization(t.ID)

		// Build speaker_info with auto_generated flags
		speakerInfo := map[string]any{}
		tsSpeakers, _ := store.GetTranscriptSpeakers(t.ID)
		for localID, name := range speakers {
			info := map[string]any{"name": name}
			if sp, ok := tsSpeakers[localID]; ok {
				info["auto_generated"] = sp.AutoGenerated
				info["id"] = sp.ID
			}
			speakerInfo[localID] = info
		}

		// Get claim tree and convert to Statement format
		tree, _ := store.GetClaimTree(t.ID)
		statements := claimTreeToStatements(tree)

		json.NewEncoder(w).Encode(map[string]any{
			"transcript":   t,
			"speakers":     speakers,
			"speaker_info": speakerInfo,
			"messages":     messages,
			"statements":   statements,
		})
		return
	}

	list, err := store.ListTranscripts()
	if err != nil {
		http.Error(w, `{"error":"db error"}`, 500)
		return
	}
	json.NewEncoder(w).Encode(list)
}

func handleAPIClaim(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/argraphments")
	path = strings.TrimPrefix(path, "/api/claims/")
	id, err := strconv.ParseInt(path, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, 400)
		return
	}
	g, err := store.GetClaimGraph(id)
	if err != nil {
		http.Error(w, `{"error":"not found"}`, 404)
		return
	}
	json.NewEncoder(w).Encode(g)
}

func handleAPISpeakers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	path = strings.TrimPrefix(path, "/argraphments")
	path = strings.TrimPrefix(path, "/api/speakers")
	path = strings.TrimPrefix(path, "/")

	if path != "" {
		name, _ := url.PathUnescape(path)

		// PUT = rename speaker
		if r.Method == http.MethodPut {
			var req struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
				jsonError(w, "invalid request", 400)
				return
			}
			sp, err := store.GetSpeakerByName(name)
			if err != nil {
				jsonError(w, "speaker not found", 404)
				return
			}
			if err := store.RenameSpeaker(sp.ID, req.Name); err != nil {
				jsonError(w, "rename failed: "+err.Error(), 500)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"name": req.Name})
			return
		}

		// GET = conversations for speaker
		convos, err := store.GetSpeakerConversations(name)
		if err != nil || convos == nil {
			json.NewEncoder(w).Encode(map[string]any{
				"name":          name,
				"conversations": []any{},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"name":          name,
			"conversations": convos,
		})
		return
	}

	speakers, err := store.ListSpeakers()
	if err != nil {
		jsonError(w, "db error", 500)
		return
	}
	if speakers == nil {
		speakers = []storage.SpeakerSummary{}
	}
	json.NewEncoder(w).Encode(speakers)
}

func handleAPIGraph(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	g, err := store.GetFullGraph()
	if err != nil {
		http.Error(w, `{"error":"db error"}`, 500)
		return
	}
	json.NewEncoder(w).Encode(g)
}

// --- Helpers ---

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Convert ClaimTreeNode to Statement format for frontend
func claimTreeToStatements(nodes []storage.ClaimTreeNode) []Statement {
	if nodes == nil {
		return nil
	}
	var result []Statement
	for _, n := range nodes {
		s := Statement{
			Speaker:  n.Speaker,
			Text:     n.Text,
			Type:     n.Type,
			MsgIndex: n.MsgIndex,
			Children: claimTreeToStatements(n.Children),
		}
		result = append(result, s)
	}
	return result
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
	SpeakerID string      `json:"speaker_id,omitempty"`
	Text      string      `json:"text"`
	Type      string      `json:"type"`
	MsgIndex  *int        `json:"msg_index,omitempty"`
	Children  []Statement `json:"children"`
	FactCheck *FactCheck  `json:"fact_check,omitempty"`
	Fallacy   *Fallacy    `json:"fallacy,omitempty"`
}

type FactCheck struct {
	Verdict     string `json:"verdict"`
	Correction  string `json:"correction"`
	SearchQuery string `json:"search_query"`
}

type Fallacy struct {
	Name        string `json:"name"`
	Explanation string `json:"explanation"`
}

type AnalysisResult struct {
	Title      string      `json:"title"`
	Statements []Statement `json:"statements"`
}

func numberTranscriptLines(transcript string) string {
	return numberTranscriptLinesOffset(transcript, 0)
}

func numberTranscriptLinesOffset(transcript string, offset int) string {
	lines := strings.Split(strings.TrimSpace(transcript), "\n")
	var sb strings.Builder
	idx := offset + 1
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("[%d] %s\n", idx, line))
		idx++
	}
	return sb.String()
}

func extractStructure(transcript string) (*AnalysisResult, error) {
	prompt := `Analyze this conversation transcript and extract a nested argument/discussion structure.

IMPORTANT — Speaker identification:
Each transcript line is pre-numbered and formatted as: [N] (speaker_id) Name: text
The number in square brackets [N] is the msg_index. The speaker_id in parentheses (e.g. "speaker_1") is the stable identifier.
Use the speaker_id for the "speaker_id" field and the display name for the "speaker" field.

Return a JSON array of top-level statements. Each statement has:
- "speaker": the display name of who said it
- "speaker_id": the speaker identifier (e.g. "speaker_1")
- "text": the core claim or statement (paraphrased concisely)
- "type": one of "claim", "response", "question", "agreement", "rebuttal", "tangent", "clarification", "evidence"
- "msg_index": the message number this statement comes from (1-based, matching the [N] labels in the transcript)
- "children": array of statements that are direct responses/follow-ups to this one
- "fact_check": ONLY include this field if the statement contains a factual claim that is false, misleading, or dubiously inaccurate based on your knowledge. Object with:
  - "verdict": one of "false", "misleading", "unverified", "mostly-true"
  - "correction": brief explanation of what's actually true
  - "search_query": a Google search query the user can use to verify
- "fallacy": ONLY include this field if the statement contains a logical fallacy. Object with:
  - "name": the name of the fallacy (e.g. "Straw Man", "Ad Hominem", "False Dichotomy", "Slippery Slope", "Appeal to Authority", "Red Herring", "Tu Quoque", "Hasty Generalization", "Circular Reasoning", "Equivocation", "Appeal to Emotion", "Anecdotal Evidence", "Cherry Picking", "Moving the Goalposts", "No True Scotsman")
  - "explanation": brief explanation of why this is a fallacy in this context

Nest responses under the statement they're responding to. A rebuttal to a claim goes as a child of that claim.

FACT-CHECKING RULES:
- Only flag objective factual claims, NOT opinions or subjective statements
- "I think X is better" = opinion, don't flag
- "X was invented in 1990" = factual, flag if wrong
- Be conservative — only flag things you're confident about
- Include fact_check field ONLY on flagged statements, omit it otherwise

FALLACY DETECTION RULES:
- Only flag clear logical fallacies, not weak arguments or disagreements
- The fallacy must be identifiable by name (not just "bad logic")
- Be conservative — only flag when the reasoning error is clear
- Include fallacy field ONLY on flagged statements, omit it otherwise

IMPORTANT RULES:
- Be CONCISE: extract the key argument from each message in 1-2 statements max, not every sentence
- NEST aggressively: responses, rebuttals, and follow-ups go as children of what they're responding to
- The msg_index MUST exactly match the [N] number from the transcript — do NOT guess or shift
- Use the speaker_id from the parentheses (e.g. "speaker_1") — do NOT confuse it with [N]

Return a JSON object with two fields:
- "title": a short, descriptive title for this conversation (5-10 words, no quotes)
- "statements": the array of top-level statements as described above

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

	text := strings.TrimSpace(result.Content[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	// Try parsing as wrapper object {title, statements}
	var analysisResult AnalysisResult
	if err := json.Unmarshal([]byte(text), &analysisResult); err == nil && len(analysisResult.Statements) > 0 {
		return &analysisResult, nil
	}

	// Fallback: bare array of statements
	var statements []Statement
	if err := json.Unmarshal([]byte(text), &statements); err != nil {
		return nil, fmt.Errorf("failed to parse structure: %v\nraw: %s", err, text)
	}

	return &AnalysisResult{Statements: statements}, nil
}

type IncrementalResult struct {
	Statements []Statement          `json:"statements"`
	Updates    []StatementUpdate    `json:"updates,omitempty"`
}

type StatementUpdate struct {
	MsgIndex    int     `json:"msg_index"`
	Text        *string `json:"text,omitempty"`
	Type        *string `json:"type,omitempty"`
	ParentText  *string `json:"parent_text,omitempty"`
}

func extractIncremental(newText string, contextText string, existing []Statement, msgOffset int, fullReview bool) (*IncrementalResult, error) {
	existingSummary := summarizeStatements(existing, 0)

	contextSection := ""
	if contextText != "" {
		contextSection = `
RECENT CONVERSATION CONTEXT (for understanding flow — already analyzed):
` + contextText + `
`
	}

	reviewSection := ""
	if fullReview {
		reviewSection = `
REVIEW MODE: In addition to analyzing new statements, review existing claims in light of the full conversation context.
If any existing claims need corrections (e.g. misattributed speaker, wrong type, should be nested differently), include them in an "updates" array.
Each update has:
- "msg_index": the msg_index of the existing statement to update
- "text": new text (only if it should change)
- "type": new type (only if it should change)  
- "parent_text": new parent to nest under (only if it should be moved)
Only include updates for claims that genuinely need fixing. Most calls should have zero updates.
`
	}

	prompt := `You are analyzing a LIVE conversation incrementally. You've already analyzed earlier parts.

EXISTING ANALYSIS (for context — do NOT repeat these):
` + existingSummary + contextSection + `
NEW PORTION to analyze (each line is pre-numbered: [N] (speaker_id) Name: text):
` + newText + `
` + reviewSection + `
Return a JSON object with:
- "statements": array of NEW statements only (from the new portion). Each has:
  - "speaker": display name
  - "speaker_id": identifier (e.g. "speaker_1")
  - "text": core claim (paraphrased concisely)
  - "type": "claim"|"response"|"question"|"agreement"|"rebuttal"|"tangent"|"clarification"|"evidence"
  - "msg_index": the [N] label number
  - "children": sub-statements array (direct responses within new text only)
  - "parent_text": text of existing statement this responds to (omit for top-level)
  - "fact_check": only if objectively false/misleading. {"verdict","correction","search_query"}
  - "fallacy": only if clear logical fallacy. {"name","explanation"}
` + func() string {
		if fullReview {
			return `- "updates": array of corrections to existing claims (empty if none needed). Each has "msg_index" plus changed fields.
`
		}
		return ""
	}() + `
Rules:
- Only NEW statements from the new text
- Empty "statements" array if no new claims
- Keep speaker labels consistent
- Only flag objective factual claims, not opinions
- Be CONCISE: summarize each message's key argument in 1-2 statements max, not every sentence
- NEST responses: if someone responds to or rebuts an existing claim, use parent_text to nest it
- The msg_index MUST match the [N] number from the transcript line — do NOT guess or shift numbers
- Use the speaker_id from the parentheses (e.g. "speaker_1") — do NOT confuse it with the [N] number

Return ONLY valid JSON object, no markdown fences.`

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

	// Try parsing as JSON object {statements, updates} first
	var objResult struct {
		Statements []json.RawMessage `json:"statements"`
		Updates    []StatementUpdate `json:"updates"`
	}
	if err := json.Unmarshal([]byte(text), &objResult); err == nil && len(objResult.Statements) > 0 || objResult.Updates != nil {
		var statements []Statement
		for _, r := range objResult.Statements {
			var s Statement
			json.Unmarshal(r, &s)
			s.Type = strings.ToLower(s.Type)
			statements = append(statements, s)
		}
		return &IncrementalResult{Statements: statements, Updates: objResult.Updates}, nil
	}

	// Fallback: bare JSON array of statements
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse error: %v", err)
	}

	var statements []Statement
	for _, r := range raw {
		var s Statement
		json.Unmarshal(r, &s)
		s.Type = strings.ToLower(s.Type)
		statements = append(statements, s)
	}

	return &IncrementalResult{Statements: statements}, nil
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

// --- Diarization ---

type DiarizeResult struct {
	Speakers map[string]string        `json:"speakers"`
	Messages []storage.DiarizeMessage `json:"messages"`
}

func assignTimestamps(messages []storage.DiarizeMessage, segments []TimedSegment) {
	segIdx := 0
	for i := range messages {
		words := strings.Fields(strings.ToLower(messages[i].Text))
		var keyWords []string
		for _, w := range words {
			if len(w) > 3 {
				keyWords = append(keyWords, w)
				if len(keyWords) >= 5 {
					break
				}
			}
		}
		if len(keyWords) == 0 {
			continue
		}
		for j := segIdx; j < len(segments) && j < segIdx+30; j++ {
			segText := strings.ToLower(segments[j].Text)
			for _, kw := range keyWords {
				if strings.Contains(segText, kw) {
					ms := segments[j].StartMs
					messages[i].StartMs = &ms
					segIdx = j
					goto nextMsg
				}
			}
		}
	nextMsg:
	}
	// Compute end_ms: each message ends when the next one starts
	for i := range messages {
		if messages[i].StartMs == nil {
			continue
		}
		if i+1 < len(messages) && messages[i+1].StartMs != nil {
			end := *messages[i+1].StartMs
			messages[i].EndMs = &end
		}
	}
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

// --- Persistence ---

func persistStatements(audioPath string, statements []Statement, speakers map[string]string, messages []storage.DiarizeMessage, speakerAutoGen map[string]bool, existingID int64) int64 {
	if store == nil {
		return 0
	}
	var tid int64
	var err error
	if existingID > 0 {
		tid = existingID
		err = store.UpdateTranscript(tid, audioPath, "")
	} else {
		tid, err = store.SaveTranscript(audioPath, "")
	}
	if err != nil {
		log.Printf("persistStatements: save transcript: %v", err)
		return 0
	}

	// Save diarization data if available
	if speakers != nil && len(messages) > 0 {
		if err := store.SaveDiarization(tid, speakers, messages); err != nil {
			log.Printf("persistStatements: save diarization: %v", err)
		}
		// Save speakers with explicit auto_generated flags from frontend
		if speakerAutoGen != nil {
			store.SaveSpeakersWithFlags(tid, speakers, speakerAutoGen)
		}
	}

	var walk func(stmts []Statement, parentClaimID *int64, pos *int)
	walk = func(stmts []Statement, parentClaimID *int64, pos *int) {
		for _, s := range stmts {
			cid, err := store.SaveClaim(s.Text, s.Type)
			if err != nil {
				log.Printf("persistStatements: save claim: %v", err)
				continue
			}
			store.SaveOccurrence(cid, tid, s.Speaker, *pos, s.Text, s.MsgIndex)
			*pos++
			if parentClaimID != nil {
				store.SaveEdge(*parentClaimID, cid, s.Type, tid)
			}
			if len(s.Children) > 0 {
				walk(s.Children, &cid, pos)
			}
		}
	}
	pos := 0
	walk(statements, nil, &pos)
	return tid
}

func countDescendants(s Statement) int {
	count := len(s.Children)
	for _, c := range s.Children {
		count += countDescendants(c)
	}
	return count
}
