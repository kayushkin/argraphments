package main

// Tests for the incremental-analysis mechanism: numbering transcript lines,
// summarizing the statements already extracted, calling the model with those
// two things, parsing what comes back, and serving the result over HTTP.
//
// Measured by the 229th nightly pass (noteboard card e9b0b89c, row
// `argraphments / extractIncremental / main.go`): before this file, a panic()
// at the first line of extractIncremental, summarizeStatements,
// numberTranscriptLines, numberTranscriptLinesOffset, countDescendants and
// handleAPIAnalyzeIncremental all left `go test ./...` green. Nothing executed
// any of them.
//
// The seam is http.DefaultClient.Transport, not a change to main.go.
// extractIncremental posts to a hardcoded https://api.anthropic.com/v1/messages
// through http.DefaultClient, so swapping the client's RoundTripper is enough
// to drive every branch without a network call, a key, or a cent of spend.
// That also makes the request itself assertable, which matters here: most of
// what this function does is BUILD A PROMPT, and the prompt is the output no
// other test could see.
//
// ⚠️ These tests mutate a process-global (http.DefaultClient.Transport), so
// none of them may call t.Parallel().

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- the seam ---------------------------------------------------------------

// fakeAnthropic answers every request with one canned Claude message envelope
// and records the request it was given.
type fakeAnthropic struct {
	replyText    string // becomes content[0].text
	status       int    // 0 means 200
	rawBody      string // if set, replaces the whole response body
	transportErr error

	gotRequest *http.Request
	gotBody    []byte
	calls      int
}

func (f *fakeAnthropic) RoundTrip(r *http.Request) (*http.Response, error) {
	f.calls++
	f.gotRequest = r
	if r.Body != nil {
		f.gotBody, _ = io.ReadAll(r.Body)
	}
	if f.transportErr != nil {
		return nil, f.transportErr
	}
	status := f.status
	if status == 0 {
		status = 200
	}
	body := f.rawBody
	if body == "" {
		env, err := json.Marshal(map[string]any{
			"content": []map[string]string{{"text": f.replyText}},
		})
		if err != nil {
			panic(err)
		}
		body = string(env)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}, nil
}

// installFakeAnthropic swaps the transport for the duration of one test.
func installFakeAnthropic(t *testing.T, f *fakeAnthropic) {
	t.Helper()
	prevTransport := http.DefaultClient.Transport
	prevKey := anthropicKey
	http.DefaultClient.Transport = f
	anthropicKey = "test-key-not-a-real-credential"
	t.Cleanup(func() {
		http.DefaultClient.Transport = prevTransport
		anthropicKey = prevKey
	})
}

// sentPrompt returns the single user message extractIncremental posted.
func (f *fakeAnthropic) sentPrompt(t *testing.T) string {
	t.Helper()
	var req struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(f.gotBody, &req); err != nil {
		t.Fatalf("request body is not JSON: %v\nbody: %s", err, f.gotBody)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected exactly 1 message, got %d", len(req.Messages))
	}
	return req.Messages[0].Content
}

// --- numberTranscriptLinesOffset --------------------------------------------

func TestNumberTranscriptLinesOffset(t *testing.T) {
	cases := []struct {
		name       string
		transcript string
		offset     int
		want       string
	}{
		{
			name:       "numbers from 1 when the offset is zero",
			transcript: "Alice: hello\nBob: hi",
			offset:     0,
			want:       "[1] Alice: hello\n[2] Bob: hi\n",
		},
		{
			name:       "the offset is the number BEFORE the first line, not the first number",
			transcript: "Alice: hello\nBob: hi",
			offset:     10,
			want:       "[11] Alice: hello\n[12] Bob: hi\n",
		},
		{
			name:       "blank lines are dropped and do NOT consume a number",
			transcript: "Alice: hello\n\n   \nBob: hi",
			offset:     0,
			want:       "[1] Alice: hello\n[2] Bob: hi\n",
		},
		{
			name:       "each line is trimmed",
			transcript: "   Alice: hello   \n\tBob: hi\t",
			offset:     0,
			want:       "[1] Alice: hello\n[2] Bob: hi\n",
		},
		{
			name:       "leading and trailing whitespace on the whole transcript is trimmed",
			transcript: "\n\n  Alice: hello  \n\n",
			offset:     0,
			want:       "[1] Alice: hello\n",
		},
		{
			name:       "a single line still gets a number",
			transcript: "Alice: hello",
			offset:     0,
			want:       "[1] Alice: hello\n",
		},
		{
			name:       "a negative offset is not clamped",
			transcript: "Alice: hello\nBob: hi",
			offset:     -1,
			want:       "[0] Alice: hello\n[1] Bob: hi\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := numberTranscriptLinesOffset(tc.transcript, tc.offset); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// An all-whitespace transcript is the one input where the "skip blank lines"
// rule and the "trim the whole thing" rule overlap. Split out because the
// answer is a single empty string and it is easy to read past in a table.
func TestNumberTranscriptLinesOffsetEmptyTranscriptProducesNoLines(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n\n", "  \n\t\n  "} {
		if got := numberTranscriptLinesOffset(in, 0); got != "" {
			t.Errorf("numberTranscriptLinesOffset(%q, 0) = %q, want %q", in, got, "")
		}
	}
}

func TestNumberTranscriptLinesUsesOffsetZero(t *testing.T) {
	transcript := "Alice: hello\nBob: hi\nCarol: hey"
	want := numberTranscriptLinesOffset(transcript, 0)
	if got := numberTranscriptLines(transcript); got != want {
		t.Errorf("numberTranscriptLines = %q, want it to equal offset-0 = %q", got, want)
	}
	if !strings.HasPrefix(numberTranscriptLines(transcript), "[1] ") {
		t.Errorf("numberTranscriptLines should start numbering at 1, got %q",
			numberTranscriptLines(transcript))
	}
}

// --- summarizeStatements ----------------------------------------------------

func TestSummarizeStatementsEmptyIsAPlaceholderNotAnEmptyString(t *testing.T) {
	// This string is pasted straight into the prompt under "EXISTING ANALYSIS".
	// An empty string there would leave a dangling header, so the placeholder
	// is load-bearing rather than cosmetic.
	if got := summarizeStatements(nil, 0); got != "(none yet)" {
		t.Errorf("summarizeStatements(nil, 0) = %q, want %q", got, "(none yet)")
	}
	if got := summarizeStatements([]Statement{}, 0); got != "(none yet)" {
		t.Errorf("summarizeStatements([], 0) = %q, want %q", got, "(none yet)")
	}
}

func TestSummarizeStatementsFormatsOneLinePerStatement(t *testing.T) {
	in := []Statement{
		{Type: "claim", Speaker: "Alice", Text: "the sky is blue"},
		{Type: "rebuttal", Speaker: "Bob", Text: "only in daylight"},
	}
	want := "- [claim] Alice: the sky is blue\n- [rebuttal] Bob: only in daylight\n"
	if got := summarizeStatements(in, 0); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSummarizeStatementsIndentsChildrenByDepth(t *testing.T) {
	in := []Statement{{
		Type: "claim", Speaker: "Alice", Text: "top",
		Children: []Statement{{
			Type: "response", Speaker: "Bob", Text: "middle",
			Children: []Statement{
				{Type: "agreement", Speaker: "Carol", Text: "bottom"},
			},
		}},
	}}
	want := "- [claim] Alice: top\n" +
		"  - [response] Bob: middle\n" +
		"    - [agreement] Carol: bottom\n"
	if got := summarizeStatements(in, 0); got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// The depth parameter indents the level it is given. Nothing in production
// passes anything but 0, so this is the only thing asserting what the argument
// means — and it is what makes the recursion above readable.
func TestSummarizeStatementsDepthArgumentIndentsTheTopLevel(t *testing.T) {
	in := []Statement{{Type: "claim", Speaker: "Alice", Text: "x"}}
	if got := summarizeStatements(in, 2); got != "    - [claim] Alice: x\n" {
		t.Errorf("depth 2 should indent by 4 spaces, got %q", got)
	}
}

func TestSummarizeStatementsWalksSiblingsAtEveryDepth(t *testing.T) {
	in := []Statement{
		{Type: "claim", Speaker: "A", Text: "one", Children: []Statement{
			{Type: "response", Speaker: "B", Text: "one-a"},
			{Type: "response", Speaker: "C", Text: "one-b"},
		}},
		{Type: "claim", Speaker: "D", Text: "two"},
	}
	got := summarizeStatements(in, 0)
	for _, want := range []string{
		"- [claim] A: one\n",
		"  - [response] B: one-a\n",
		"  - [response] C: one-b\n",
		"- [claim] D: two\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\ngot:\n%s", want, got)
		}
	}
	// A child must never outrank its parent in the output order.
	if strings.Index(got, "one-a") > strings.Index(got, "two") {
		t.Errorf("children must precede the next sibling, got:\n%s", got)
	}
}

// --- countDescendants -------------------------------------------------------

func TestCountDescendants(t *testing.T) {
	leaf := Statement{Text: "leaf"}
	if got := countDescendants(leaf); got != 0 {
		t.Errorf("a leaf has 0 descendants, got %d", got)
	}

	oneLevel := Statement{Children: []Statement{{Text: "a"}, {Text: "b"}}}
	if got := countDescendants(oneLevel); got != 2 {
		t.Errorf("two children = 2, got %d", got)
	}

	// Counts the whole subtree, not just direct children — the distinction the
	// name makes and the only thing that can be wrong here.
	nested := Statement{Children: []Statement{
		{Text: "a", Children: []Statement{
			{Text: "a1"},
			{Text: "a2", Children: []Statement{{Text: "a2i"}}},
		}},
		{Text: "b"},
	}}
	if got := countDescendants(nested); got != 5 {
		t.Errorf("subtree of 5 = 5, got %d", got)
	}
}

// --- extractIncremental: the request it builds ------------------------------

func TestExtractIncrementalPostsToAnthropicWithTheExpectedHeaders(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] (speaker_1) Alice: hi", "", nil, 0, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("expected exactly 1 upstream call, got %d", fake.calls)
	}
	r := fake.gotRequest
	if r.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", r.Method)
	}
	if got := r.URL.String(); got != "https://api.anthropic.com/v1/messages" {
		t.Errorf("url = %s", got)
	}
	if got := r.Header.Get("x-api-key"); got != "test-key-not-a-real-credential" {
		t.Errorf("x-api-key = %q, want the package key to be sent", got)
	}
	if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q", got)
	}
	if got := r.Header.Get("content-type"); got != "application/json" {
		t.Errorf("content-type = %q", got)
	}

	var body map[string]any
	if err := json.Unmarshal(fake.gotBody, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model = %v", body["model"])
	}
	if body["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
}

func TestExtractIncrementalPromptCarriesTheNewTextAndExistingSummary(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	existing := []Statement{{Type: "claim", Speaker: "Alice", Text: "the sky is blue"}}
	if _, err := extractIncremental("[7] (speaker_2) Bob: no it is not", "", existing, 0, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	prompt := fake.sentPrompt(t)

	if !strings.Contains(prompt, "[7] (speaker_2) Bob: no it is not") {
		t.Error("prompt is missing the new text")
	}
	if !strings.Contains(prompt, "- [claim] Alice: the sky is blue") {
		t.Error("prompt is missing the summary of existing statements")
	}
	if !strings.Contains(prompt, "EXISTING ANALYSIS") || !strings.Contains(prompt, "NEW PORTION") {
		t.Error("prompt is missing its section headers")
	}
}

// An empty contextText must omit the whole section rather than emit an empty
// header — the difference between "no context" and "the context is blank".
func TestExtractIncrementalOmitsTheContextSectionWhenThereIsNoContext(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] (speaker_1) A: hi", "", nil, 0, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(fake.sentPrompt(t), "RECENT CONVERSATION CONTEXT") {
		t.Error("empty contextText must not emit the context header")
	}
}

func TestExtractIncrementalIncludesTheContextSectionWhenGivenOne(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] (speaker_1) A: hi", "earlier chatter", nil, 0, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	prompt := fake.sentPrompt(t)
	if !strings.Contains(prompt, "RECENT CONVERSATION CONTEXT") {
		t.Error("context header missing")
	}
	if !strings.Contains(prompt, "earlier chatter") {
		t.Error("context body missing")
	}
	if !strings.Contains(prompt, "already analyzed") {
		t.Error("the context header must tell the model this text is already analysed")
	}
}

// --- extractIncremental: fullReview, the census's bool parameter ------------

// fullReview is the parameter this row was found by: neither value was
// exercised. It has TWO separate effects on the prompt — the REVIEW MODE block
// and an extra "updates" bullet in the return-shape list — and they are built
// by different pieces of code (a plain string and an inline func literal), so
// each gets its own assertion.
func TestExtractIncrementalFullReviewFalseOmitsBothReviewEdits(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] (speaker_1) A: hi", "", nil, 0, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	prompt := fake.sentPrompt(t)
	if strings.Contains(prompt, "REVIEW MODE") {
		t.Error("fullReview=false must not emit the REVIEW MODE block")
	}
	if strings.Contains(prompt, `"updates": array of corrections`) {
		t.Error("fullReview=false must not ask for an updates array in the return shape")
	}
}

func TestExtractIncrementalFullReviewTrueAddsBothReviewEdits(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] (speaker_1) A: hi", "", nil, 0, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	prompt := fake.sentPrompt(t)
	if !strings.Contains(prompt, "REVIEW MODE") {
		t.Error("fullReview=true must emit the REVIEW MODE block")
	}
	if !strings.Contains(prompt, `"updates": array of corrections`) {
		t.Error("fullReview=true must ask for an updates array in the return shape")
	}
	// The REVIEW MODE block documents the update record; if it drifts out of
	// step with StatementUpdate the model is told the wrong field names.
	for _, field := range []string{"msg_index", "text", "type", "parent_text"} {
		if !strings.Contains(prompt, field) {
			t.Errorf("REVIEW MODE block should document the %q field", field)
		}
	}
}

// The two prompts must differ ONLY by the review additions. This is what stops
// a future edit moving a shared instruction inside the fullReview branch.
func TestExtractIncrementalReviewModeOnlyAddsToThePrompt(t *testing.T) {
	shared := func(fullReview bool) string {
		fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
		installFakeAnthropic(t, fake)
		if _, err := extractIncremental("[1] (speaker_1) A: hi", "ctx", nil, 0, fullReview); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return fake.sentPrompt(t)
	}
	plain, review := shared(false), shared(true)
	if len(review) <= len(plain) {
		t.Fatalf("review prompt (%d) should be longer than plain (%d)", len(review), len(plain))
	}
	// Every line of the plain prompt must survive into the review prompt.
	for _, line := range strings.Split(plain, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(review, line) {
			t.Errorf("review mode dropped a shared prompt line: %q", line)
		}
	}
}

// --- extractIncremental: parsing the reply ----------------------------------

func TestExtractIncrementalParsesTheObjectForm(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[
		{"speaker":"Alice","speaker_id":"speaker_1","text":"the sky is blue","type":"claim","msg_index":7},
		{"speaker":"Bob","speaker_id":"speaker_2","text":"no","type":"rebuttal","msg_index":8}
	],"updates":[]}`}
	installFakeAnthropic(t, fake)

	got, err := extractIncremental("[7] x", "", nil, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Statements) != 2 {
		t.Fatalf("want 2 statements, got %d", len(got.Statements))
	}
	if got.Statements[0].Speaker != "Alice" || got.Statements[0].SpeakerID != "speaker_1" {
		t.Errorf("speaker fields not parsed: %+v", got.Statements[0])
	}
	if got.Statements[0].MsgIndex == nil || *got.Statements[0].MsgIndex != 7 {
		t.Errorf("msg_index not parsed: %+v", got.Statements[0].MsgIndex)
	}
	if got.Statements[1].Type != "rebuttal" {
		t.Errorf("type = %q", got.Statements[1].Type)
	}
}

func TestExtractIncrementalParsesTheBareArrayFallback(t *testing.T) {
	fake := &fakeAnthropic{replyText: `[{"speaker":"Alice","text":"hi","type":"claim"}]`}
	installFakeAnthropic(t, fake)

	got, err := extractIncremental("[1] x", "", nil, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Statements) != 1 || got.Statements[0].Speaker != "Alice" {
		t.Fatalf("bare array not parsed: %+v", got.Statements)
	}
	if got.Updates != nil {
		t.Errorf("the bare-array form carries no updates, got %+v", got.Updates)
	}
}

func TestExtractIncrementalParsesUpdatesWithTheirOptionalFields(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[
		{"msg_index":3,"text":"corrected text"},
		{"msg_index":4,"type":"rebuttal","parent_text":"the sky is blue"},
		{"msg_index":5}
	]}`}
	installFakeAnthropic(t, fake)

	got, err := extractIncremental("[1] x", "", nil, 0, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Updates) != 3 {
		t.Fatalf("want 3 updates, got %d", len(got.Updates))
	}
	// The pointer fields are how "leave this alone" is distinguished from
	// "set this to the empty string". A non-pointer would collapse the two.
	if got.Updates[0].Text == nil || *got.Updates[0].Text != "corrected text" {
		t.Errorf("update 0 text = %v", got.Updates[0].Text)
	}
	if got.Updates[0].Type != nil {
		t.Errorf("update 0 omitted type, want nil, got %v", *got.Updates[0].Type)
	}
	if got.Updates[1].Type == nil || *got.Updates[1].Type != "rebuttal" {
		t.Errorf("update 1 type = %v", got.Updates[1].Type)
	}
	if got.Updates[1].ParentText == nil || *got.Updates[1].ParentText != "the sky is blue" {
		t.Errorf("update 1 parent_text = %v", got.Updates[1].ParentText)
	}
	if got.Updates[2].Text != nil || got.Updates[2].Type != nil || got.Updates[2].ParentText != nil {
		t.Errorf("update 2 should be all-nil, got %+v", got.Updates[2])
	}
	if got.Updates[2].MsgIndex != 5 {
		t.Errorf("update 2 msg_index = %d", got.Updates[2].MsgIndex)
	}
}

func TestExtractIncrementalLowercasesStatementTypes(t *testing.T) {
	// The frontend switches on the type string, so "CLAIM" and "claim" must
	// not be two different things. Covers both parse paths.
	t.Run("object form", func(t *testing.T) {
		fake := &fakeAnthropic{replyText: `{"statements":[{"text":"a","type":"REBUTTAL"}],"updates":[]}`}
		installFakeAnthropic(t, fake)
		got, err := extractIncremental("[1] x", "", nil, 0, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Statements[0].Type != "rebuttal" {
			t.Errorf("type = %q, want lowercased", got.Statements[0].Type)
		}
	})
	t.Run("bare array form", func(t *testing.T) {
		fake := &fakeAnthropic{replyText: `[{"text":"a","type":"Clarification"}]`}
		installFakeAnthropic(t, fake)
		got, err := extractIncremental("[1] x", "", nil, 0, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Statements[0].Type != "clarification" {
			t.Errorf("type = %q, want lowercased", got.Statements[0].Type)
		}
	})
}

// ⚠️ Only the TOP level is lowercased. Nested children keep whatever case the
// model sent, because the walk never recurses. Pinned as the behaviour that is
// actually there — if this ever starts failing, the recursion was added and
// the frontend can stop case-folding.
func TestExtractIncrementalDoesNotLowercaseNestedChildTypes(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[
		{"text":"parent","type":"CLAIM","children":[{"text":"child","type":"REBUTTAL"}]}
	],"updates":[]}`}
	installFakeAnthropic(t, fake)

	got, err := extractIncremental("[1] x", "", nil, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Statements[0].Type != "claim" {
		t.Errorf("top-level type = %q, want lowercased", got.Statements[0].Type)
	}
	if got.Statements[0].Children[0].Type != "REBUTTAL" {
		t.Errorf("child type = %q — if this now reads %q the recursion was added; "+
			"update the frontend's case-folding accordingly",
			got.Statements[0].Children[0].Type, "rebuttal")
	}
}

func TestExtractIncrementalStripsMarkdownFences(t *testing.T) {
	cases := map[string]string{
		"json fence":         "```json\n{\"statements\":[{\"text\":\"a\",\"type\":\"claim\"}],\"updates\":[]}\n```",
		"bare fence":         "```\n{\"statements\":[{\"text\":\"a\",\"type\":\"claim\"}],\"updates\":[]}\n```",
		"fence plus padding": "  \n```json\n{\"statements\":[{\"text\":\"a\",\"type\":\"claim\"}],\"updates\":[]}\n```  \n",
		"fenced bare array":  "```json\n[{\"text\":\"a\",\"type\":\"claim\"}]\n```",
	}
	for name, reply := range cases {
		t.Run(name, func(t *testing.T) {
			fake := &fakeAnthropic{replyText: reply}
			installFakeAnthropic(t, fake)
			got, err := extractIncremental("[1] x", "", nil, 0, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got.Statements) != 1 || got.Statements[0].Text != "a" {
				t.Fatalf("fence not stripped: %+v", got.Statements)
			}
		})
	}
}

// --- extractIncremental: the failure paths ----------------------------------

func TestExtractIncrementalReturnsTheUpstreamStatusAndBody(t *testing.T) {
	fake := &fakeAnthropic{status: 429, rawBody: `{"error":{"message":"rate limited"}}`}
	installFakeAnthropic(t, fake)

	_, err := extractIncremental("[1] x", "", nil, 0, false)
	if err == nil {
		t.Fatal("a non-200 must be an error")
	}
	// The status and the upstream body are the only diagnosis available to
	// whoever reads the 500 this becomes.
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should name the status: %v", err)
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should carry the upstream body: %v", err)
	}
}

func TestExtractIncrementalRejectsAnEmptyContentArray(t *testing.T) {
	fake := &fakeAnthropic{rawBody: `{"content":[]}`}
	installFakeAnthropic(t, fake)

	_, err := extractIncremental("[1] x", "", nil, 0, false)
	if err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("want an empty-response error, got %v", err)
	}
}

func TestExtractIncrementalRejectsAnUnparseableEnvelope(t *testing.T) {
	fake := &fakeAnthropic{rawBody: `this is not json`}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] x", "", nil, 0, false); err == nil {
		t.Fatal("a non-JSON envelope must be an error")
	}
}

func TestExtractIncrementalPropagatesATransportError(t *testing.T) {
	fake := &fakeAnthropic{transportErr: errFakeTransport{}}
	installFakeAnthropic(t, fake)

	if _, err := extractIncremental("[1] x", "", nil, 0, false); err == nil {
		t.Fatal("a transport failure must be an error, not an empty result")
	}
}

type errFakeTransport struct{}

func (errFakeTransport) Error() string { return "dial failed" }

func TestExtractIncrementalRejectsAReplyThatIsNeitherObjectNorArray(t *testing.T) {
	fake := &fakeAnthropic{replyText: `"just a string"`}
	installFakeAnthropic(t, fake)

	_, err := extractIncremental("[1] x", "", nil, 0, false)
	if err == nil || !strings.Contains(err.Error(), "parse error") {
		t.Fatalf("want a parse error, got %v", err)
	}
}

// --- ⛔ the two defects, pinned as characterisations -------------------------

// DEFECT 1 — noteboard card 1d4b1f9c.
//
// The reply `{"statements": []}` is EXACTLY what this function's own prompt
// asks for: `- Empty "statements" array if no new claims`. And outside review
// mode the prompt never mentions "updates" at all, so a compliant model omits
// it. The branch condition is
//
//	if err := json.Unmarshal(...); err == nil && len(objResult.Statements) > 0 || objResult.Updates != nil {
//
// which Go groups as `(err == nil && len > 0) || (Updates != nil)`. With zero
// statements and no updates key BOTH disjuncts are false, so control falls
// through to the bare-array fallback, which cannot parse an object — and the
// caller answers HTTP 500.
//
// So the single commonest outcome of incremental analysis — a new chunk with
// nothing new in it — is reported to the client as a server error.
//
// This test pins the CURRENT behaviour. When the defect is fixed it will fail;
// that is the signal to change it to expect a zero-statement success.
func TestExtractIncrementalEmptyStatementsObjectIsRejected_DEFECT(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements": []}`}
	installFakeAnthropic(t, fake)

	got, err := extractIncremental("[1] (speaker_1) A: nothing new here", "", nil, 0, false)
	if err == nil {
		t.Fatalf("DEFECT 1 (noteboard 1d4b1f9c) appears FIXED — "+
			"`{\"statements\": []}` now succeeds with %+v. "+
			"Change this test to assert a zero-statement success and close the card.", got)
	}
	if !strings.Contains(err.Error(), "parse error") {
		t.Errorf("expected the fallback's parse error, got %v", err)
	}
	// The same payload WITH an updates key succeeds, which is what makes this a
	// bug rather than a policy: the two replies mean the same thing.
	fake2 := &fakeAnthropic{replyText: `{"statements": [], "updates": []}`}
	installFakeAnthropic(t, fake2)
	ok, err := extractIncremental("[1] (speaker_1) A: nothing new here", "", nil, 0, false)
	if err != nil {
		t.Fatalf("the same reply with an updates key should succeed, got %v", err)
	}
	if len(ok.Statements) != 0 {
		t.Errorf("want 0 statements, got %d", len(ok.Statements))
	}
}

// DEFECT 2 — noteboard card 1d4b1f9c, second half. Same missing parentheses,
// opposite direction.
//
// Because `objResult.Updates != nil` is its own disjunct, it is read even when
// the unmarshal FAILED. Go's decoder fills fields as it walks, so a reply whose
// `updates` parses and whose `statements` does not leaves Updates non-nil with
// err non-nil — and the branch is taken anyway. The unmarshal error is
// discarded, the malformed statements are silently dropped, and the caller gets
// HTTP 200 with an empty statements array.
//
// A whole turn of conversation disappears and nothing anywhere says so.
func TestExtractIncrementalMalformedStatementsAreSilentlyDropped_DEFECT(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"updates": [{"msg_index": 3}], "statements": "not-an-array"}`}
	installFakeAnthropic(t, fake)

	got, err := extractIncremental("[1] (speaker_1) A: hi", "", nil, 0, true)
	if err != nil {
		t.Fatalf("DEFECT 2 (noteboard 1d4b1f9c) appears FIXED — the malformed "+
			"statements field is now an error (%v). Change this test to expect "+
			"that error and close the card.", err)
	}
	if len(got.Statements) != 0 {
		t.Fatalf("expected the malformed statements to have been dropped, got %d", len(got.Statements))
	}
	if len(got.Updates) != 1 || got.Updates[0].MsgIndex != 3 {
		t.Errorf("the updates half parsed and should survive, got %+v", got.Updates)
	}
	t.Log("pinned: a malformed `statements` field returns nil error and zero statements")
}

// DEFECT 3 — noteboard card ba0eb70b. `msgOffset` is accepted by the endpoint
// as `msg_offset`, threaded through handleAPIAnalyzeIncremental, and then never
// read by extractIncremental. Its sibling `numberTranscriptLinesOffset` — the
// only thing that could apply it — has no caller that passes anything but 0.
//
// Pinned by showing that two calls differing ONLY in msgOffset build a
// byte-identical request.
func TestExtractIncrementalIgnoresMsgOffset_DEFECT(t *testing.T) {
	send := func(offset int) []byte {
		fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
		installFakeAnthropic(t, fake)
		if _, err := extractIncremental("[1] (speaker_1) A: hi", "", nil, offset, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return fake.gotBody
	}
	if !bytes.Equal(send(0), send(500)) {
		t.Fatalf("DEFECT 3 (noteboard ba0eb70b) appears FIXED — msgOffset now " +
			"changes the request. Assert what it does and close the card.")
	}
	t.Log("pinned: msgOffset does not affect the request in any way")
}

// --- handleAPIAnalyzeIncremental --------------------------------------------

func TestHandleAPIAnalyzeIncrementalRejectsNonPost(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		w := httptest.NewRecorder()
		handleAPIAnalyzeIncremental(w, httptest.NewRequest(method, "/api/analyze/incremental", nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: got %d, want 405", method, w.Code)
		}
	}
}

func TestHandleAPIAnalyzeIncrementalRejectsInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental",
		strings.NewReader(`{"new_text": `))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleAPIAnalyzeIncremental(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid JSON") {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestHandleAPIAnalyzeIncrementalRejectsEmptyNewText(t *testing.T) {
	// Whitespace-only counts as empty — otherwise the model is billed for a
	// prompt with nothing in it.
	for _, newText := range []string{"", "   ", "\n\t "} {
		body, err := json.Marshal(map[string]any{"new_text": newText})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handleAPIAnalyzeIncremental(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("new_text=%q: got %d, want 400", newText, w.Code)
		}
		if !strings.Contains(w.Body.String(), "no new_text") {
			t.Errorf("new_text=%q: body = %q", newText, w.Body.String())
		}
	}
}

// The empty-new_text guard must run BEFORE the upstream call, or a blank
// request still costs money.
func TestHandleAPIAnalyzeIncrementalDoesNotCallUpstreamForEmptyNewText(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[],"updates":[]}`}
	installFakeAnthropic(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental",
		strings.NewReader(`{"new_text":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	handleAPIAnalyzeIncremental(httptest.NewRecorder(), req)

	if fake.calls != 0 {
		t.Errorf("upstream was called %d times for an empty new_text; want 0", fake.calls)
	}
}

func TestHandleAPIAnalyzeIncrementalReturnsTheParsedResult(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[
		{"speaker":"Alice","text":"the sky is blue","type":"claim","msg_index":7}
	],"updates":[{"msg_index":3,"text":"fixed"}]}`}
	installFakeAnthropic(t, fake)

	body, err := json.Marshal(map[string]any{
		"new_text":     "[7] (speaker_1) Alice: the sky is blue",
		"context_text": "earlier",
		"existing":     []Statement{{Type: "claim", Speaker: "Bob", Text: "prior"}},
		"full_review":  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleAPIAnalyzeIncremental(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var got IncrementalResult
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if len(got.Statements) != 1 || got.Statements[0].Speaker != "Alice" {
		t.Errorf("statements = %+v", got.Statements)
	}
	if len(got.Updates) != 1 || got.Updates[0].MsgIndex != 3 {
		t.Errorf("updates = %+v", got.Updates)
	}

	// The handler must forward every field it decoded, not just new_text.
	prompt := fake.sentPrompt(t)
	if !strings.Contains(prompt, "earlier") {
		t.Error("context_text was not forwarded")
	}
	if !strings.Contains(prompt, "- [claim] Bob: prior") {
		t.Error("existing was not forwarded")
	}
	if !strings.Contains(prompt, "REVIEW MODE") {
		t.Error("full_review was not forwarded")
	}
}

func TestHandleAPIAnalyzeIncrementalTurnsAnExtractErrorInto500(t *testing.T) {
	fake := &fakeAnthropic{status: 500, rawBody: `upstream exploded`}
	installFakeAnthropic(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental",
		strings.NewReader(`{"new_text":"[1] (speaker_1) A: hi"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleAPIAnalyzeIncremental(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "upstream exploded") {
		t.Errorf("the upstream body should reach the client: %q", w.Body.String())
	}
}

// ⚠️ The multipart branch reads ONLY new_text and existing. context_text,
// msg_offset and full_review are silently dropped on this path, so a form
// client cannot request review mode at all. Pinned as the behaviour that is
// there.
func TestHandleAPIAnalyzeIncrementalMultipartReadsOnlyNewTextAndExisting(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[{"text":"a","type":"claim"}],"updates":[]}`}
	installFakeAnthropic(t, fake)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("new_text", "[1] (speaker_1) A: hi"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("existing", `[{"type":"claim","speaker":"Bob","text":"prior"}]`); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("context_text", "should be ignored"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("full_review", "true"); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	handleAPIAnalyzeIncremental(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	prompt := fake.sentPrompt(t)
	if !strings.Contains(prompt, "- [claim] Bob: prior") {
		t.Error("multipart `existing` was not forwarded")
	}
	if strings.Contains(prompt, "should be ignored") {
		t.Error("multipart context_text is documented as dropped but reached the prompt")
	}
	if strings.Contains(prompt, "REVIEW MODE") {
		t.Error("multipart full_review is documented as dropped but reached the prompt")
	}
}

// A multipart body whose `existing` field is malformed is swallowed by an
// unchecked json.Unmarshal in the handler. The request still succeeds with no
// existing context — pinned so the silent drop is at least visible here.
func TestHandleAPIAnalyzeIncrementalMultipartMalformedExistingIsIgnored(t *testing.T) {
	fake := &fakeAnthropic{replyText: `{"statements":[{"text":"a","type":"claim"}],"updates":[]}`}
	installFakeAnthropic(t, fake)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("new_text", "[1] (speaker_1) A: hi"); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("existing", `{not json`); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/analyze/incremental", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	handleAPIAnalyzeIncremental(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(fake.sentPrompt(t), "(none yet)") {
		t.Error("a malformed `existing` should leave the summary empty")
	}
}
