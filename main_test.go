package main

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func init() {
	anthropicKey = os.Getenv("ANTHROPIC_API_KEY")
	openaiKey = os.Getenv("OPENAI_API_KEY")
	os.Setenv("BASE_PATH", "/argraphments")
	os.MkdirAll("uploads", 0755)

	var err error
	templates, err = loadTemplates()
	if err != nil {
		panic("failed to load templates: " + err.Error())
	}
}

func setupMux() *http.ServeMux {
	prefix := "/argraphments"
	mux := http.NewServeMux()
	mux.HandleFunc(prefix+"/", handleIndex)
	mux.HandleFunc(prefix+"/transcribe", handleTranscribe)
	mux.HandleFunc(prefix+"/analyze", handleAnalyze)
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

func TestAnalyzePasteText(t *testing.T) {
	if anthropicKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	mux := setupMux()
	form := url.Values{}
	form.Set("transcript", `
Speaker 1: I think we should use Go for the backend.
Speaker 2: Why not Python? It's faster to prototype.
Speaker 1: Go compiles to a single binary, deployment is way simpler.
Speaker 2: Fair point, but Python has more ML libraries.
Speaker 1: We're not doing ML though, it's just a web server.
Speaker 2: OK, I'm convinced. Let's go with Go.
`)

	req := httptest.NewRequest("POST", "/argraphments/analyze", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, `class="error"`) {
		t.Fatalf("got error: %s", body)
	}
	if !strings.Contains(body, "argument-tree") {
		t.Fatalf("response missing argument tree: %s", body)
	}
	if !strings.Contains(body, "<details>") {
		t.Fatalf("response missing nested details elements: %s", body)
	}
	t.Logf("Analyze response length: %d bytes", len(body))
}

func TestTranscribeAudioUpload(t *testing.T) {
	if openaiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}

	// Generate a small WAV file with silence (valid audio)
	wavData := generateSilentWAV(1) // 1 second

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("audio", "test.wav")
	if err != nil {
		t.Fatal(err)
	}
	part.Write(wavData)
	writer.Close()

	mux := setupMux()
	req := httptest.NewRequest("POST", "/argraphments/transcribe", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "transcript") && !strings.Contains(body, "Transcript") {
		t.Fatalf("response doesn't look like transcript result: %s", body)
	}
	t.Logf("Transcribe response length: %d bytes", len(body))
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
