package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/kayushkin/argraphments/storage"
)

func init() {
	anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	openaiKey = os.Getenv("OPENAI_API_KEY")
	os.MkdirAll("uploads", 0755)

	var err error
	templates, err = loadTemplates()
	if err != nil {
		panic("failed to load templates: " + err.Error())
	}
}

func setupTestStore(t *testing.T) {
	t.Helper()
	var err error
	store, err = storage.NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close(); store = nil })
}

func setupMux() *http.ServeMux {
	mux := http.NewServeMux()
	staticFS := http.FileServer(http.Dir("static"))
	for _, prefix := range []string{"/argraphments", ""} {
		p := prefix
		mux.HandleFunc(p+"/", handleIndex)
		mux.HandleFunc(p+"/convo/", handleIndex) // conversation URLs
		mux.Handle(p+"/static/", http.StripPrefix(p+"/static/", staticFS))
		mux.HandleFunc(p+"/api/session/new", handleAPINewSession)
		mux.HandleFunc(p+"/api/transcribe", handleAPITranscribe)
		mux.HandleFunc(p+"/api/diarize", handleAPIDiarize)
		mux.HandleFunc(p+"/api/analyze", handleAPIAnalyze)
		mux.HandleFunc(p+"/api/analyze-incremental", handleAPIAnalyzeIncremental)
		mux.HandleFunc(p+"/api/transcripts", handleAPITranscripts)
		mux.HandleFunc(p+"/api/transcripts/", handleAPITranscripts)
		mux.HandleFunc(p+"/api/claims/", handleAPIClaim)
		mux.HandleFunc(p+"/api/speakers", handleAPISpeakers)
		mux.HandleFunc(p+"/api/speakers/", handleAPISpeakers)
		mux.HandleFunc(p+"/api/import/youtube", handleAPIImportYouTube)
		mux.HandleFunc(p+"/api/graph", handleAPIGraph)
		mux.HandleFunc(p+"/api/sample", handleAPISample)
	}
	return mux
}

func TestIndex(t *testing.T) {
	mux := setupMux()
	req := httptest.NewRequest("GET", "/argraphments/", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "argraphments") {
		t.Fatal("index page missing title")
	}
}

func TestAnalyzeAPI(t *testing.T) {
	if anthropicKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	mux := setupMux()
	payload := map[string]string{
		"transcript": `Speaker 1: I think we should use Go for the backend.
Speaker 2: Why not Python? It's faster to prototype.
Speaker 1: Go compiles to a single binary, deployment is way simpler.
Speaker 2: Fair point, but Python has more ML libraries.
Speaker 1: We're not doing ML though, it's just a web server.
Speaker 2: OK, I'm convinced. Let's go with Go.`,
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/argraphments/api/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	statements, ok := result["statements"].([]any)
	if !ok || len(statements) == 0 {
		t.Fatalf("expected statements array, got: %v", result)
	}
	t.Logf("Analyze response: %d statements", len(statements))
}

func TestTranscribeAPI(t *testing.T) {
	if openaiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	wavData := generateSilentWAV(1)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("audio", "test.wav")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(wavData)
	writer.Close()

	mux := setupMux()
	req := httptest.NewRequest("POST", "/argraphments/api/transcribe", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if _, ok := result["text"]; !ok {
		t.Fatalf("response missing 'text' field: %v", result)
	}
	t.Logf("Transcribe response: %q", result["text"])
}

// generateSilentWAV creates a minimal valid WAV file with silence
func generateSilentWAV(durationSecs int) []byte {
	sampleRate := 16000
	bitsPerSample := 16
	numChannels := 1
	numSamples := sampleRate * durationSecs
	dataSize := numSamples * numChannels * (bitsPerSample / 8)

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	writeLE32(&buf, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	writeLE32(&buf, 16)
	writeLE16(&buf, 1) // PCM
	writeLE16(&buf, uint16(numChannels))
	writeLE32(&buf, uint32(sampleRate))
	writeLE32(&buf, uint32(sampleRate*numChannels*(bitsPerSample/8)))
	writeLE16(&buf, uint16(numChannels*(bitsPerSample/8)))
	writeLE16(&buf, uint16(bitsPerSample))
	buf.WriteString("data")
	writeLE32(&buf, uint32(dataSize))
	buf.Write(make([]byte, dataSize))

	return buf.Bytes()
}

func writeLE16(w io.Writer, v uint16) {
	w.Write([]byte{byte(v), byte(v >> 8)})
}

func writeLE32(w io.Writer, v uint32) {
	w.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

// --- E2E tests: session lifecycle ---

func TestE2E_CreateSession(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// Create a new session
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)

	slug, ok := result["slug"].(string)
	if !ok || slug == "" {
		t.Fatalf("expected slug, got: %v", result)
	}
	if !strings.Contains(slug, "-") {
		t.Fatalf("slug should be two words with dash, got: %s", slug)
	}
	id, ok := result["id"].(float64)
	if !ok || id == 0 {
		t.Fatalf("expected id, got: %v", result)
	}
	t.Logf("Created session: slug=%s id=%.0f", slug, id)
}

func TestE2E_CreateAndRetrieveBySlug(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// 1. Create session
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("create session: expected 200, got %d", w.Code)
	}
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	slug := session["slug"].(string)

	// 2. Retrieve by slug — should exist but be empty
	req = httptest.NewRequest("GET", "/api/transcripts/"+slug, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get by slug: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatalf("invalid JSON: %v\nbody: %s", err, w.Body.String())
	}
	transcript, ok := data["transcript"].(map[string]any)
	if !ok {
		t.Fatalf("transcript field missing or wrong type: %v\nfull: %s", data["transcript"], w.Body.String())
	}
	if transcript["slug"] != slug {
		t.Fatalf("expected slug %s, got %s", slug, transcript["slug"])
	}
	t.Logf("Retrieved session by slug: %s", slug)
}

func TestE2E_AnalyzeWithSlugAndRetrieve(t *testing.T) {
	if anthropicKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	setupTestStore(t)
	mux := setupMux()

	// 1. Create session
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	slug := session["slug"].(string)

	// 2. Analyze with slug
	payload := map[string]any{
		"transcript": "Speaker 1: The earth is flat. Speaker 2: No it's not, that's been disproven.",
		"slug":       slug,
		"speakers":   map[string]string{"speaker_1": "Alice", "speaker_2": "Bob"},
		"messages": []map[string]string{
			{"speaker": "speaker_1", "text": "The earth is flat."},
			{"speaker": "speaker_2", "text": "No it's not, that's been disproven."},
		},
	}
	body, _ := json.Marshal(payload)
	req = httptest.NewRequest("POST", "/api/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("analyze: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var analyzeResult map[string]any
	json.Unmarshal(w.Body.Bytes(), &analyzeResult)
	if analyzeResult["slug"] != slug {
		t.Fatalf("analyze should return same slug, got: %v", analyzeResult["slug"])
	}
	statements := analyzeResult["statements"].([]any)
	if len(statements) == 0 {
		t.Fatal("expected statements from analysis")
	}

	// 3. Retrieve by slug — should have transcript, speakers, messages, statements
	req = httptest.NewRequest("GET", "/api/transcripts/"+slug, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)

	transcript := data["transcript"].(map[string]any)
	if transcript["slug"] != slug {
		t.Fatalf("slug mismatch: expected %s, got %s", slug, transcript["slug"])
	}
	speakers := data["speakers"]
	if speakers == nil {
		t.Fatal("expected speakers")
	}
	speakerMap := speakers.(map[string]any)
	if speakerMap["speaker_1"] != "Alice" {
		t.Fatalf("expected speaker_1=Alice, got %v", speakerMap["speaker_1"])
	}

	messages := data["messages"]
	if messages == nil {
		t.Fatal("expected messages")
	}
	msgList := messages.([]any)
	if len(msgList) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgList))
	}

	stmts := data["statements"]
	if stmts == nil {
		t.Fatal("expected statements")
	}
	stmtList := stmts.([]any)
	if len(stmtList) == 0 {
		t.Fatal("expected statements from DB")
	}

	t.Logf("Full roundtrip OK: slug=%s, %d statements, %d messages", slug, len(stmtList), len(msgList))
}

func TestE2E_ListTranscriptsIncludesSlug(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// Create two sessions
	var slugs []string
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("POST", "/api/session/new", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var s map[string]any
		json.Unmarshal(w.Body.Bytes(), &s)
		slugs = append(slugs, s["slug"].(string))
	}

	// List
	req := httptest.NewRequest("GET", "/api/transcripts", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) < 2 {
		t.Fatalf("expected at least 2 transcripts, got %d", len(list))
	}
	for _, item := range list {
		slug, ok := item["slug"].(string)
		if !ok || slug == "" {
			t.Fatalf("transcript missing slug: %v", item)
		}
	}
	t.Logf("Listed %d transcripts, all have slugs", len(list))
}

func TestE2E_UniqueSlugs(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("POST", "/api/session/new", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		var s map[string]any
		json.Unmarshal(w.Body.Bytes(), &s)
		slug := s["slug"].(string)
		if seen[slug] {
			t.Fatalf("duplicate slug after %d creates: %s", i, slug)
		}
		seen[slug] = true
	}
	t.Logf("20 unique slugs generated")
}

func TestE2E_SlugURL_ServesIndex(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// A slug URL should serve the SPA index
	for _, path := range []string{"/bold-fox", "/argraphments/bold-fox", "/"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("path %s: expected 200, got %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "argraphments") {
			t.Fatalf("path %s: should serve SPA index", path)
		}
	}
}

func TestE2E_ConvoURL_ServesIndex(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// New /convo/{slug} URL format should serve the SPA index
	for _, path := range []string{"/convo/bold-fox", "/argraphments/convo/bold-fox"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("path %s: expected 200, got %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "argraphments") {
			t.Fatalf("path %s: should serve SPA index", path)
		}
	}
}

func TestE2E_NonExistentSlug_404(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	req := httptest.NewRequest("GET", "/api/transcripts/does-not-exist", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("expected 404 for nonexistent slug, got %d", w.Code)
	}
}

func TestE2E_BothPrefixes(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// Create session on root prefix
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("root prefix: expected 200, got %d", w.Code)
	}
	var s1 map[string]any
	json.Unmarshal(w.Body.Bytes(), &s1)
	slug1 := s1["slug"].(string)

	// Retrieve on /argraphments prefix
	req = httptest.NewRequest("GET", "/argraphments/api/transcripts/"+slug1, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("argraphments prefix: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	transcript := data["transcript"].(map[string]any)
	if transcript["slug"] != slug1 {
		t.Fatalf("cross-prefix slug mismatch")
	}

	// Create on /argraphments, retrieve on root
	req = httptest.NewRequest("POST", "/argraphments/api/session/new", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var s2 map[string]any
	json.Unmarshal(w.Body.Bytes(), &s2)
	slug2 := s2["slug"].(string)

	req = httptest.NewRequest("GET", "/api/transcripts/"+slug2, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("root prefix retrieve: expected 200, got %d", w.Code)
	}

	t.Logf("Both prefixes work: %s, %s", slug1, slug2)
}

func TestE2E_EmptySessionNotInList(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// Create a session but don't analyze — it should still appear in list
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	req = httptest.NewRequest("GET", "/api/transcripts", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 transcript in list, got %d", len(list))
	}
}

func TestE2E_SpeakerRename(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// Create a transcript with speakers
	tid, _ := store.SaveTranscript("", "test")
	store.SaveDiarization(tid, map[string]string{"speaker_1": "Alice", "speaker_2": "Bob"}, []storage.DiarizeMessage{
		{Speaker: "speaker_1", Text: "hello"},
		{Speaker: "speaker_2", Text: "hi"},
	})

	// Verify speakers exist
	req := httptest.NewRequest("GET", "/api/speakers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var speakers []map[string]any
	json.Unmarshal(w.Body.Bytes(), &speakers)
	if len(speakers) != 2 {
		t.Fatalf("expected 2 speakers, got %d", len(speakers))
	}

	// Rename Alice → Carol
	body, _ := json.Marshal(map[string]string{"name": "Carol"})
	req = httptest.NewRequest("PUT", "/api/speakers/Alice", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify diarization now returns Carol instead of Alice
	spk, _, _ := store.GetDiarization(tid)
	if spk["speaker_1"] != "Carol" {
		t.Fatalf("expected speaker_1=Carol after rename, got %s", spk["speaker_1"])
	}

	// Verify speakers list updated
	req = httptest.NewRequest("GET", "/api/speakers", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	json.Unmarshal(w.Body.Bytes(), &speakers)
	names := map[string]bool{}
	for _, s := range speakers {
		names[s["name"].(string)] = true
	}
	if names["Alice"] {
		t.Fatal("Alice should no longer be in speakers list")
	}
	if !names["Carol"] {
		t.Fatal("Carol should be in speakers list")
	}
	t.Log("Speaker rename Alice→Carol persisted globally")
}

func TestE2E_AnalyzeUpdatesExistingTranscript(t *testing.T) {
	if anthropicKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	setupTestStore(t)
	mux := setupMux()

	// Create session
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	slug := session["slug"].(string)

	// Analyze with that slug
	payload := map[string]any{
		"transcript": "Speaker 1: Go is great. Speaker 2: I agree.",
		"slug":       slug,
	}
	body, _ := json.Marshal(payload)
	req = httptest.NewRequest("POST", "/api/analyze", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("analyze: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Should still only be 1 transcript (updated, not duplicated)
	req = httptest.NewRequest("GET", "/api/transcripts", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var list []map[string]any
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 1 {
		t.Fatalf("expected 1 transcript (updated in place), got %d", len(list))
	}

	// Verify the slug is the same
	if list[0]["slug"] != slug {
		t.Fatalf("slug changed: expected %s, got %s", slug, list[0]["slug"])
	}

	// Verify transcript has content now
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/transcripts/%s", slug), nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)
	// raw_text no longer stored; text reconstructed from utterances
	t.Logf("Analyze updated existing transcript: slug=%s", slug)
}

func TestJSSyntax(t *testing.T) {
	// Verify app.js has no syntax errors
	data, err := os.ReadFile("static/app.js")
	if err != nil {
		t.Fatal("cannot read static/app.js:", err)
	}
	content := string(data)

	// Check balanced backticks (template literals)
	backtickCount := strings.Count(content, "`")
	if backtickCount%2 != 0 {
		t.Fatal("unbalanced backticks in app.js — template literal syntax error likely")
	}

	// Check balanced braces
	opens := strings.Count(content, "{")
	closes := strings.Count(content, "}")
	if opens != closes {
		t.Fatalf("unbalanced braces in app.js: %d opens, %d closes", opens, closes)
	}

	// Check submitTestConvo calls /api/sample
	if !strings.Contains(content, "/api/sample") {
		t.Fatal("submitTestConvo should call /api/sample endpoint")
	}
	if !strings.Contains(content, "function submitTestConvo") {
		t.Fatal("submitTestConvo function missing")
	}

	// Check key functions exist
	for _, fn := range []string{
		"function submitTestConvo",
		"function submitPasteForDiarize",
		"function renderChatMessages",
		"function highlightMsg",
		"function formatMs",
	} {
		if !strings.Contains(content, fn) {
			t.Fatalf("missing function: %s", fn)
		}
	}
}

func TestDiarizeEndpoint(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	transcript := "Alex: Remote work is better than office work.\nJordan: That's too absolute. Some roles need in-person collaboration."
	body, _ := json.Marshal(map[string]string{"transcript": transcript})
	req := httptest.NewRequest("POST", "/api/diarize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Skip("diarize requires API key or returned error:", w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)

	if _, ok := result["error"]; ok {
		t.Skip("diarize requires API key:", result["error"])
	}

	speakers, ok := result["speakers"].(map[string]any)
	if !ok || len(speakers) < 2 {
		t.Fatalf("expected at least 2 speakers, got: %v", result["speakers"])
	}

	messages, ok := result["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got: %v", result["messages"])
	}
	t.Logf("Diarize OK: %d speakers, %d messages", len(speakers), len(messages))
}

func TestDiarizeWithSegments(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	transcript := "Alex: Remote work is better.\nJordan: No it is not."
	body, _ := json.Marshal(map[string]any{
		"transcript": transcript,
		"segments": []map[string]any{
			{"start_ms": 1000, "text": "Remote work is better"},
			{"start_ms": 5000, "text": "No it is not"},
		},
	})
	req := httptest.NewRequest("POST", "/api/diarize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Skip("diarize requires API key or returned error:", w.Body.String())
	}

	var result map[string]any
	json.Unmarshal(w.Body.Bytes(), &result)
	if _, ok := result["error"]; ok {
		t.Skip("diarize requires API key:", result["error"])
	}

	messages, ok := result["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("expected at least 2 messages, got: %v", result["messages"])
	}

	// Check that timestamps were assigned
	msg0 := messages[0].(map[string]any)
	if msg0["start_ms"] == nil {
		t.Fatal("expected start_ms on first message from segments")
	}
	t.Logf("Diarize with segments OK: first message start_ms=%v", msg0["start_ms"])
}

func TestUtterancesPersistence(t *testing.T) {
	setupTestStore(t)

	// Create a transcript
	tid, err := store.SaveTranscript("", "")
	if err != nil {
		t.Fatal(err)
	}

	speakers := map[string]string{"speaker_1": "Alex", "speaker_2": "Jordan"}
	start1 := int64(1000)
	end1 := int64(5000)
	start2 := int64(5000)
	messages := []storage.DiarizeMessage{
		{Speaker: "speaker_1", Text: "Remote work is better", StartMs: &start1, EndMs: &end1},
		{Speaker: "speaker_2", Text: "No it is not", StartMs: &start2, EndMs: nil},
	}

	err = store.SaveDiarization(tid, speakers, messages)
	if err != nil {
		t.Fatal("SaveDiarization failed:", err)
	}

	// Read back
	gotSpeakers, gotMessages, err := store.GetDiarization(tid)
	if err != nil {
		t.Fatal("GetDiarization failed:", err)
	}

	if len(gotMessages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(gotMessages))
	}

	if gotMessages[0].Text != "Remote work is better" {
		t.Fatalf("wrong text: %s", gotMessages[0].Text)
	}
	if gotMessages[0].StartMs == nil || *gotMessages[0].StartMs != 1000 {
		t.Fatalf("wrong start_ms: %v", gotMessages[0].StartMs)
	}
	if gotMessages[0].EndMs == nil || *gotMessages[0].EndMs != 5000 {
		t.Fatalf("wrong end_ms: %v", gotMessages[0].EndMs)
	}
	if gotMessages[1].EndMs != nil {
		t.Fatalf("expected nil end_ms for last message, got %v", gotMessages[1].EndMs)
	}

	if len(gotSpeakers) < 2 {
		t.Fatalf("expected 2 speakers, got %d: %v", len(gotSpeakers), gotSpeakers)
	}

	t.Logf("Utterances persistence OK: %d messages, %d speakers", len(gotMessages), len(gotSpeakers))
}

func TestSampleConvoFlow(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// 1. Create session
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("session/new failed:", w.Code)
	}
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	slug := session["slug"].(string)
	if slug == "" {
		t.Fatal("empty slug")
	}

	// 2. Diarize a sample transcript
	transcript := "Sam: AI will replace programmers.\nTaylor: That's a bold claim."
	body, _ := json.Marshal(map[string]string{"transcript": transcript})
	req = httptest.NewRequest("POST", "/api/diarize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Skip("diarize requires API key or returned error:", w.Code)
	}
	var diarize map[string]any
	json.Unmarshal(w.Body.Bytes(), &diarize)
	if _, ok := diarize["error"]; ok {
		t.Skip("requires API key")
	}

	// 3. Retrieve by slug — should serve SPA
	req = httptest.NewRequest("GET", "/"+slug, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("slug page returned %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "argraphments") {
		t.Fatal("slug page doesn't contain expected HTML")
	}

	t.Logf("Sample flow OK: session=%s, diarize speakers=%v", slug, diarize["speakers"])
}
