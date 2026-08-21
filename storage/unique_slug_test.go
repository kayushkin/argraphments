package storage

import (
	"strings"
	"testing"
)

// seedSlugs writes n adjective-noun slugs straight into transcripts, walking the
// generator's own vocabulary so the seeded names are exactly the ones
// generateSlug can produce. It returns the set it took.
func seedSlugs(t *testing.T, s *Store, n int) map[string]bool {
	t.Helper()
	taken := map[string]bool{}
	for _, a := range slugAdjectives {
		for _, noun := range slugNouns {
			if len(taken) >= n {
				return taken
			}
			slug := a + "-" + noun
			if _, err := s.db.Exec(
				`INSERT INTO transcripts (slug, audio_path, title) VALUES (?, ?, ?)`,
				slug, "/seed.mp3", "seed",
			); err != nil {
				t.Fatalf("seeding slug %q: %v", slug, err)
			}
			taken[slug] = true
		}
	}
	return taken
}

// TestSaveTranscriptSkipsSlugsAlreadyInTheTable pins uniqueSlug's collision
// check.
//
// Why it is written this way. transcripts.slug carries a UNIQUE constraint, and
// the index answers LAST — after uniqueSlug has already handed the INSERT a
// name. So a test that asserts only "an error came back" cannot tell the Go
// check from the index: with the check deleted the row is refused all the same,
// just by a different layer and with a message naming neither the transcript nor
// the slug. This test therefore asserts the side effect only the Go ordering
// produces: with half the vocabulary already taken, every SaveTranscript still
// SUCCEEDS.
//
// The existing TestE2E_UniqueSlugs cannot do this job. It makes 20 draws against
// a 50x50 space, so deleting the collision check leaves it green in 28 runs out
// of 30 (measured), and on the 2 it does redden it dies by nil-interface panic
// rather than reaching its own "duplicate slug" message — the handler returns no
// slug at all once the index refuses the row.
func TestSaveTranscriptSkipsSlugsAlreadyInTheTable(t *testing.T) {
	s := newTestStore(t)

	space := len(slugAdjectives) * len(slugNouns)
	taken := seedSlugs(t, s, space/2)

	// 40 draws against a half-full space. Real code retries past a taken name,
	// so all 40 land. Without the check each draw is a coin flip, and 40
	// consecutive misses is a 1-in-10^12 event.
	const draws = 40
	seen := map[string]bool{}
	for i := 0; i < draws; i++ {
		id, err := s.SaveTranscript("/audio.mp3", "collision probe")
		if err != nil {
			t.Fatalf("draw %d of %d: SaveTranscript failed with %d of %d slugs taken: %v — "+
				"uniqueSlug handed the INSERT a name that was already in the table and "+
				"transcripts.slug's UNIQUE index refused the row, which is the index doing "+
				"the Go check's job", i+1, draws, len(taken), space, err)
		}
		tr, err := s.GetTranscript(id)
		if err != nil {
			t.Fatalf("draw %d: reading back id %d: %v", i+1, id, err)
		}
		if taken[tr.Slug] {
			t.Fatalf("draw %d: SaveTranscript reused seeded slug %q", i+1, tr.Slug)
		}
		if seen[tr.Slug] {
			t.Fatalf("draw %d: SaveTranscript reused slug %q from an earlier draw", i+1, tr.Slug)
		}
		seen[tr.Slug] = true
	}
}

// TestSaveTranscriptStillWritesARowWhenEveryGeneratedNameIsTaken pins the
// fallback arm, which nothing else reaches: once all 2,500 adjective-noun pairs
// are in the table, uniqueSlug's 100 attempts all collide and it must fall
// through to the timestamped name. Nothing before this test exercised that arm,
// so a change that dropped it would have been invisible.
func TestSaveTranscriptStillWritesARowWhenEveryGeneratedNameIsTaken(t *testing.T) {
	s := newTestStore(t)

	space := len(slugAdjectives) * len(slugNouns)
	taken := seedSlugs(t, s, space)
	if len(taken) != space {
		t.Fatalf("seeded %d slugs, want the whole %d-name space", len(taken), space)
	}

	id, err := s.SaveTranscript("/audio.mp3", "space exhausted")
	if err != nil {
		t.Fatalf("SaveTranscript failed with the whole %d-name space taken: %v — "+
			"uniqueSlug found no free name and did not fall back to a timestamped one", space, err)
	}
	tr, err := s.GetTranscript(id)
	if err != nil {
		t.Fatal(err)
	}
	if taken[tr.Slug] {
		t.Fatalf("SaveTranscript reused taken slug %q instead of falling back", tr.Slug)
	}
	// The fallback is the bare name plus "-" and up to four digits.
	if !strings.Contains(tr.Slug, "-") || len(strings.Split(tr.Slug, "-")) != 3 {
		t.Fatalf("slug %q is not the timestamped fallback shape adjective-noun-digits", tr.Slug)
	}
}
