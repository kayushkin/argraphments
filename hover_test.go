package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kayushkin/argraphments/storage"
)

// TestHoverHighlightDataAttributes verifies that:
// 1. Saved conversations return msg_index on statements
// 2. Saved conversations return position on messages  
// 3. The rendered HTML contains data-msg-idx attributes on chat messages
// 4. The JS sets up delegated hover listeners (not inline handlers)
func TestHoverHighlightDataAttributes(t *testing.T) {
	setupTestStore(t)
	mux := setupMux()

	// Create a session
	req := httptest.NewRequest("POST", "/api/session/new", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var session map[string]any
	json.Unmarshal(w.Body.Bytes(), &session)
	slug := session["slug"].(string)

	// Save some utterances directly
	tid := int64(session["id"].(float64))
	start1 := int64(0)
	end1 := int64(5000)
	start2 := int64(5000)
	end2 := int64(10000)
	store.SaveDiarization(tid, map[string]string{
		"speaker_1": "Alex",
		"speaker_2": "Jordan",
	}, []storage.DiarizeMessage{
		{Speaker: "speaker_1", Text: "AI will replace jobs", StartMs: &start1, EndMs: &end1},
		{Speaker: "speaker_2", Text: "Not all jobs", StartMs: &start2, EndMs: &end2},
	})

	// Save claims with msg_index
	cid1, _ := store.SaveClaim("AI will replace jobs", "claim")
	cid2, _ := store.SaveClaim("Not all jobs will be replaced", "rebuttal")
	msgIdx1 := 1
	msgIdx2 := 2
	store.SaveOccurrence(cid1, tid, "Alex", 0, "AI will replace jobs", &msgIdx1)
	store.SaveOccurrence(cid2, tid, "Jordan", 1, "Not all jobs", &msgIdx2)
	store.SaveEdge(cid1, cid2, "rebuttal", tid)

	// Fetch the saved conversation
	req = httptest.NewRequest("GET", "/api/transcripts/"+slug, nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("GET transcript returned %d: %s", w.Code, w.Body.String())
	}

	var data map[string]any
	json.Unmarshal(w.Body.Bytes(), &data)

	// Check messages have position
	messages, ok := data["messages"].([]any)
	if !ok || len(messages) < 2 {
		t.Fatalf("expected 2 messages, got: %v", data["messages"])
	}
	msg1 := messages[0].(map[string]any)
	if msg1["position"] == nil {
		t.Fatal("message missing position field")
	}
	pos := int(msg1["position"].(float64))
	if pos < 1 {
		t.Fatalf("expected 1-based position, got %d", pos)
	}
	t.Logf("Message 1 position: %d", pos)

	// Check statements have msg_index
	statements, ok := data["statements"].([]any)
	if !ok || len(statements) < 1 {
		t.Fatalf("expected statements, got: %v", data["statements"])
	}
	stmt1 := statements[0].(map[string]any)
	if stmt1["msg_index"] == nil {
		t.Fatal("statement missing msg_index field")
	}
	msgIdx := int(stmt1["msg_index"].(float64))
	if msgIdx != 1 {
		t.Fatalf("expected msg_index=1, got %d", msgIdx)
	}

	// Check children also have msg_index
	children, ok := stmt1["children"].([]any)
	if !ok || len(children) < 1 {
		t.Fatalf("expected children on first statement, got: %v", stmt1["children"])
	}
	child1 := children[0].(map[string]any)
	if child1["msg_index"] == nil {
		t.Fatal("child statement missing msg_index field")
	}

	t.Logf("Statement msg_index=%d, child msg_index=%v", msgIdx, child1["msg_index"])

	// Verify the JS has delegated listeners, not inline handlers
	req = httptest.NewRequest("GET", "/static/app.js", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	js := w.Body.String()

	// Should NOT have inline onmouseenter on chat-msg
	if strings.Contains(js, `onmouseenter="highlightMsg`) {
		t.Fatal("chat-msg still has inline onmouseenter — should use delegated listener")
	}

	// Should have delegated mouseover on tree
	if !strings.Contains(js, `tree.addEventListener('mouseover'`) {
		t.Fatal("missing delegated mouseover on tree")
	}

	// Should have delegated mouseover on chat container
	if !strings.Contains(js, `container.addEventListener('mouseover'`) {
		t.Fatal("missing delegated mouseover on chat container")
	}

	// Should use closest() for innermost match
	if !strings.Contains(js, `.closest('.statement[data-msg-idx]')`) {
		t.Fatal("tree hover should use closest('.statement[data-msg-idx]')")
	}
	if !strings.Contains(js, `.closest('.chat-msg[data-msg-idx]')`) {
		t.Fatal("chat hover should use closest('.chat-msg[data-msg-idx]')")
	}

	t.Log("Hover highlight test passed: delegated listeners, msg_index in API, positions on messages")
}
