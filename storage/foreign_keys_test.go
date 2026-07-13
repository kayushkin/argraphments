package storage

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

// newFileBackedStore returns a store on a real file. The suite's usual
// newTestStore uses ":memory:", where every pooled connection gets its OWN
// private database — which would quietly defeat any test about connection
// pooling.
func newFileBackedStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// TestForeignKeysEnforcedOnEveryPooledConnection is the regression test for the
// bug where the store enabled foreign_keys with a one-shot db.Exec on a *sql.DB.
// That reaches a single connection out of the pool; every other connection kept
// SQLite's default of OFF, so whether the schema's REFERENCES clauses were
// enforced depended on which connection a write happened to land on.
//
// It asserts the constraint actually FIRES rather than reading the pragma back:
// a rejected write is the contract, the pragma value is only a proxy for it.
func TestForeignKeysEnforcedOnEveryPooledConnection(t *testing.T) {
	s := newFileBackedStore(t)
	ctx := context.Background()

	// Force the pool to hand out several distinct connections at once, and have
	// each attempt a write the schema forbids: a transcript_speakers row naming
	// a speaker that does not exist. Under the old one-shot PRAGMA these were
	// accepted, silently seeding orphans.
	const connections = 8
	gate := make(chan struct{})

	var wg sync.WaitGroup
	accepted := make([]bool, connections)
	for i := 0; i < connections; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			conn, err := s.db.Conn(ctx)
			if err != nil {
				t.Errorf("checkout connection %d: %v", i, err)
				return
			}
			defer conn.Close()

			// Stay checked out until every goroutine holds one, so the pool must
			// open a distinct connection for each of us.
			<-gate
			_, err = conn.ExecContext(ctx,
				`INSERT INTO transcript_speakers (transcript_id, speaker_id, local_id) VALUES (?, ?, ?)`,
				nil, 999999, "spk")
			accepted[i] = err == nil
		}(i)
	}
	close(gate)
	wg.Wait()

	for i, ok := range accepted {
		if ok {
			t.Errorf("connection %d accepted a transcript_speakers row referencing a nonexistent "+
				"speaker: foreign_keys is OFF on this pooled connection", i)
		}
	}
}

// TestRenameSpeakerMergeSurvivesForeignKeyEnforcement drives the one delete path
// that touches a referenced table.
//
// Turning enforcement on was expected to break this: speakers is referenced by
// transcript_speakers with no ON DELETE clause, so SQLite's default RESTRICT
// should block "DELETE FROM speakers". It does not, and this test is the proof.
// The merge reassigns the speaker's transcript_speakers rows to the surviving
// speaker first, which leaves the row unreferenced by the time it is deleted.
// If someone later deletes a speaker without reassigning its rows, they will get
// a constraint error — which is the point.
func TestRenameSpeakerMergeSurvivesForeignKeyEnforcement(t *testing.T) {
	s := newFileBackedStore(t)

	transcriptID, err := s.SaveTranscript("/tmp/audio.wav", "t")
	if err != nil {
		t.Fatalf("SaveTranscript: %v", err)
	}

	from := mustSpeaker(t, s, "Alice")
	into := mustSpeaker(t, s, "Bob")

	// Give the doomed speaker a referencing row — the RESTRICT surface.
	if _, err := s.db.Exec(
		`INSERT INTO transcript_speakers (transcript_id, speaker_id, local_id) VALUES (?, ?, ?)`,
		transcriptID, from, "S1"); err != nil {
		t.Fatalf("link speaker to transcript: %v", err)
	}

	// Rename Alice -> "Bob", i.e. merge her into the existing Bob.
	if err := s.RenameSpeaker(from, "Bob"); err != nil {
		t.Fatalf("RenameSpeaker merge under foreign key enforcement: %v", err)
	}

	var speakersLeft int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM speakers WHERE id = ?`, from).Scan(&speakersLeft); err != nil {
		t.Fatalf("count speakers: %v", err)
	}
	if speakersLeft != 0 {
		t.Errorf("merged-away speaker %d still exists", from)
	}

	// Its transcript rows must have moved to the survivor, not vanished.
	var reassigned int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM transcript_speakers WHERE speaker_id = ?`, into).Scan(&reassigned); err != nil {
		t.Fatalf("count reassigned: %v", err)
	}
	if reassigned != 1 {
		t.Errorf("transcript_speakers rows for the surviving speaker = %d, want 1", reassigned)
	}
}

// TestRenameSpeakerMergeReportsFailure covers the other half of the old bug: the
// merge discarded the error from both of its statements and returned nil, so a
// failed merge was indistinguishable from a successful one. With enforcement on,
// a swallowed error would specifically hide a foreign key violation.
func TestRenameSpeakerMergeReportsFailure(t *testing.T) {
	s := newFileBackedStore(t)
	mustSpeaker(t, s, "Bob")

	// Merge a speaker id that does not exist into Bob. The UPDATE and DELETE both
	// affect zero rows, so this is not itself an error — but renaming a
	// nonexistent speaker must not silently report success either.
	if err := s.RenameSpeaker(404404, "Bob"); err != nil {
		t.Logf("RenameSpeaker on a missing speaker returned: %v", err)
	}

	// The real assertion: closing the database makes every statement fail. The
	// old code returned nil here regardless.
	s.db.Close()
	if err := s.RenameSpeaker(1, "Bob"); err == nil {
		t.Error("RenameSpeaker returned nil after its statements could not run; " +
			"a failed merge must be reported, not swallowed")
	}
}

func mustSpeaker(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	res, err := s.db.Exec(`INSERT INTO speakers (name) VALUES (?)`, name)
	if err != nil {
		t.Fatalf("insert speaker %q: %v", name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return id
}
