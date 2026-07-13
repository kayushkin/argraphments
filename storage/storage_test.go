package storage

import (
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNormHash(t *testing.T) {
	h1 := NormHash("Hello World")
	h2 := NormHash("  hello   world  ")
	h3 := NormHash("different")
	if h1 != h2 {
		t.Error("expected same hash for normalized equivalent strings")
	}
	if h1 == h3 {
		t.Error("expected different hash")
	}
}

func TestSaveAndGetTranscript(t *testing.T) {
	s := newTestStore(t)
	id, err := s.SaveTranscript("/audio.mp3", "Test Title")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatal("expected positive id")
	}

	tr, err := s.GetTranscript(id)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title != "Test Title" {
		t.Errorf("unexpected transcript: %+v", tr)
	}
}

func TestListTranscripts(t *testing.T) {
	s := newTestStore(t)
	s.SaveTranscript("", "A")
	s.SaveTranscript("", "B")
	list, err := s.ListTranscripts()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
}

func TestSaveClaimDedup(t *testing.T) {
	s := newTestStore(t)
	id1, err := s.SaveClaim("The sky is blue", "claim")
	if err != nil {
		t.Fatal(err)
	}
	id2, err := s.SaveClaim("  the sky  is  blue ", "claim")
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("expected dedup: %d != %d", id1, id2)
	}
}

func TestSaveOccurrenceAndEdge(t *testing.T) {
	s := newTestStore(t)
	tid, _ := s.SaveTranscript("", "T")
	cid1, _ := s.SaveClaim("claim 1", "claim")
	cid2, _ := s.SaveClaim("claim 2", "rebuttal")

	if err := s.SaveOccurrence(cid1, tid, "Alice", 0, "claim 1 original", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOccurrence(cid2, tid, "Bob", 1, "claim 2 original", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveEdge(cid1, cid2, "rebuttal", tid); err != nil {
		t.Fatal(err)
	}
}

func TestGetClaimByHash(t *testing.T) {
	s := newTestStore(t)
	s.SaveClaim("unique claim", "claim")
	hash := NormHash("unique claim")
	c, err := s.GetClaimByHash(hash)
	if err != nil {
		t.Fatal(err)
	}
	if c.Text != "unique claim" {
		t.Errorf("unexpected: %s", c.Text)
	}
}

func TestGetClaimTree(t *testing.T) {
	s := newTestStore(t)
	tid, _ := s.SaveTranscript("", "T")
	cid1, _ := s.SaveClaim("parent claim", "claim")
	cid2, _ := s.SaveClaim("child claim", "rebuttal")
	s.SaveOccurrence(cid1, tid, "A", 0, "parent", nil)
	s.SaveOccurrence(cid2, tid, "B", 1, "child", nil)
	s.SaveEdge(cid1, cid2, "rebuttal", tid)

	tree, err := s.GetClaimTree(tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected 1 root, got %d", len(tree))
	}
	if len(tree[0].Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree[0].Children))
	}
}

func TestGetClaimGraph(t *testing.T) {
	s := newTestStore(t)
	tid, _ := s.SaveTranscript("", "T")
	cid1, _ := s.SaveClaim("claim A", "claim")
	cid2, _ := s.SaveClaim("claim B", "rebuttal")
	s.SaveOccurrence(cid1, tid, "A", 0, "orig A", nil)
	s.SaveEdge(cid1, cid2, "rebuttal", tid)

	g, err := s.GetClaimGraph(cid1)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Occurrences) != 1 {
		t.Errorf("expected 1 occurrence, got %d", len(g.Occurrences))
	}
	if len(g.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(g.Edges))
	}
}

func TestGetFullGraph(t *testing.T) {
	s := newTestStore(t)
	tid, _ := s.SaveTranscript("", "T")
	cid1, _ := s.SaveClaim("c1", "claim")
	cid2, _ := s.SaveClaim("c2", "claim")
	s.SaveEdge(cid1, cid2, "supports", tid)

	g, err := s.GetFullGraph()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Claims) != 2 {
		t.Errorf("expected 2 claims, got %d", len(g.Claims))
	}
	if len(g.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(g.Edges))
	}
}
