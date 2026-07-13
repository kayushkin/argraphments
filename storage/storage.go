package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var slugAdjectives = []string{
	"swift", "calm", "bold", "warm", "cool", "bright", "dark", "soft", "loud", "quick",
	"slow", "deep", "high", "low", "wild", "tame", "sharp", "flat", "round", "thin",
	"thick", "light", "heavy", "fresh", "stale", "sweet", "sour", "crisp", "fuzzy", "smooth",
	"rough", "dry", "wet", "hot", "cold", "pale", "vivid", "plain", "fancy", "cozy",
	"snug", "vast", "tiny", "grand", "shy", "keen", "lazy", "brave", "gentle", "fierce",
}

var slugNouns = []string{
	"oak", "fox", "owl", "elk", "bee", "ant", "bat", "cat", "dog", "eel",
	"fig", "gem", "hat", "ice", "jam", "koi", "log", "mug", "nut", "orb",
	"pea", "ram", "sun", "tea", "urn", "vine", "yak", "zinc", "arch", "bell",
	"cave", "dawn", "fern", "gale", "haze", "iris", "jade", "knot", "lamp", "moth",
	"nest", "opal", "pine", "reef", "sage", "tide", "wave", "bloom", "creek", "dusk",
}

func generateSlug() string {
	a := slugAdjectives[rand.Intn(len(slugAdjectives))]
	n := slugNouns[rand.Intn(len(slugNouns))]
	return a + "-" + n
}

type Store struct {
	db *sql.DB
}

type Transcript struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	RawText   string    `json:"raw_text"` // deprecated: reconstructed from utterances
	AudioPath string    `json:"audio_path"`
	Title     string    `json:"title"`
	SourceURL string    `json:"source_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type Claim struct {
	ID        int64     `json:"id"`
	Text      string    `json:"text"`
	Type      string    `json:"type"`
	NormHash  string    `json:"norm_hash"`
	CreatedAt time.Time `json:"created_at"`
}

type Occurrence struct {
	ID           int64  `json:"id"`
	ClaimID      int64  `json:"claim_id"`
	TranscriptID int64  `json:"transcript_id"`
	Speaker      string `json:"speaker"`
	Position     int    `json:"position"`
	OriginalText string `json:"original_text"`
}

type Edge struct {
	ID           int64  `json:"id"`
	ParentID     int64  `json:"parent_id"`
	ChildID      int64  `json:"child_id"`
	Relation     string `json:"relation"`
	TranscriptID int64  `json:"transcript_id"`
}

// For API responses
type ClaimWithDetails struct {
	Claim
	Occurrences []OccurrenceWithTranscript `json:"occurrences"`
	Edges       []EdgeWithClaims           `json:"edges"`
}

type OccurrenceWithTranscript struct {
	Occurrence
	TranscriptTitle string `json:"transcript_title"`
}

type EdgeWithClaims struct {
	Edge
	ParentText string `json:"parent_text"`
	ChildText  string `json:"child_text"`
}

type ClaimTreeNode struct {
	Claim
	Speaker      string          `json:"speaker"`
	OriginalText string          `json:"original_text"`
	MsgIndex     *int            `json:"msg_index,omitempty"`
	Children     []ClaimTreeNode `json:"children"`
}

type GraphData struct {
	Claims []Claim `json:"claims"`
	Edges  []Edge  `json:"edges"`
}

type DiarizeMessage struct {
	Speaker  string `json:"speaker"`
	Text     string `json:"text"`
	Position int    `json:"position,omitempty"`
	StartMs  *int64 `json:"start_ms,omitempty"`
	EndMs    *int64 `json:"end_ms,omitempty"`
}

var collapseSpaces = regexp.MustCompile(`\s+`)

func NormHash(text string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	normalized = collapseSpaces.ReplaceAllString(normalized, " ")
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)
}

// dataSourceName builds the sqlite DSN for a database path.
//
// foreign_keys MUST be set here rather than with a PRAGMA statement after
// Open: it is a per-connection setting, and *sql.DB is a pool. A one-shot
// db.Exec("PRAGMA foreign_keys=ON") reaches only whichever connection the
// pool happened to hand out; every other connection keeps SQLite's default
// of OFF, so the REFERENCES clauses in our schema are enforced or not
// depending on which connection a write lands on. Putting it in the DSN
// applies it to every connection the pool opens. (journal_mode=WAL needs no
// such care: it is a property of the database file, so one Exec is permanent.)
//
// _pragma=... is modernc's syntax and it is the only one that works here.
// The mattn/go-sqlite3 spelling (_foreign_keys=on) is silently ignored by
// this driver, as is any other unrecognised key — hence the check in NewStore.
func dataSourceName(dbPath string) string {
	return dbPath + "?_pragma=foreign_keys(1)"
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dataSourceName(dbPath))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	// An unrecognised DSN key is silently ignored by the driver, so prove the
	// setting actually took effect instead of trusting the connection string.
	var foreignKeysEnabled bool
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeysEnabled); err != nil {
		db.Close()
		return nil, fmt.Errorf("read foreign_keys pragma: %w", err)
	}
	if !foreignKeysEnabled {
		db.Close()
		return nil, fmt.Errorf("foreign key enforcement is off: %q did not enable it", dataSourceName(dbPath))
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS transcripts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			slug        TEXT UNIQUE,
			raw_text    TEXT,
			audio_path  TEXT,
			title       TEXT,
			source_url  TEXT DEFAULT '',
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS claims (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			text        TEXT,
			type        TEXT,
			norm_hash   TEXT UNIQUE,
			created_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS occurrences (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			claim_id        INTEGER REFERENCES claims(id),
			transcript_id   INTEGER REFERENCES transcripts(id),
			speaker         TEXT,
			position        INTEGER,
			original_text   TEXT,
			msg_index       INTEGER
		);
		CREATE TABLE IF NOT EXISTS edges (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			parent_id       INTEGER REFERENCES claims(id),
			child_id        INTEGER REFERENCES claims(id),
			relation        TEXT,
			transcript_id   INTEGER REFERENCES transcripts(id)
		);
		CREATE TABLE IF NOT EXISTS utterances (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			transcript_id   INTEGER REFERENCES transcripts(id),
			speaker         TEXT NOT NULL,
			text            TEXT NOT NULL,
			position        INTEGER NOT NULL,
			start_ms        INTEGER,
			end_ms          INTEGER
		);
		CREATE TABLE IF NOT EXISTS speakers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT UNIQUE NOT NULL,
			auto_generated  INTEGER DEFAULT 0,
			created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS transcript_speakers (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			transcript_id   INTEGER REFERENCES transcripts(id),
			speaker_id      INTEGER REFERENCES speakers(id),
			local_id        TEXT NOT NULL,
			UNIQUE(transcript_id, local_id)
		);
	`)
	if err != nil {
		return err
	}
	// Migrations
	s.db.Exec("ALTER TABLE transcripts ADD COLUMN slug TEXT UNIQUE")
	s.db.Exec("ALTER TABLE speakers ADD COLUMN auto_generated INTEGER DEFAULT 0")
	s.db.Exec("ALTER TABLE transcripts ADD COLUMN source_url TEXT DEFAULT ''")
	s.db.Exec("ALTER TABLE occurrences ADD COLUMN msg_index INTEGER")

	// Migrate old diarizations JSON table → utterances rows
	migrateRows, migrateErr := s.db.Query("SELECT transcript_id, speakers_json, messages_json FROM diarizations")
	if migrateErr == nil {
		defer migrateRows.Close()
		for migrateRows.Next() {
			var tid int64
			var speakersStr, messagesStr string
			if migrateRows.Scan(&tid, &speakersStr, &messagesStr) == nil {
				var msgs []DiarizeMessage
				var speakers map[string]string
				json.Unmarshal([]byte(messagesStr), &msgs)
				json.Unmarshal([]byte(speakersStr), &speakers)
				if len(msgs) > 0 {
					s.SaveDiarization(tid, speakers, msgs)
				}
			}
		}
		s.db.Exec("DROP TABLE IF EXISTS diarizations")
	}
	// Backfill any transcripts missing slugs
	rows, err := s.db.Query("SELECT id FROM transcripts WHERE slug IS NULL OR slug = ''")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			slug := s.uniqueSlug()
			s.db.Exec("UPDATE transcripts SET slug = ? WHERE id = ?", slug, id)
		}
	}
	return nil
}

func (s *Store) uniqueSlug() string {
	for i := 0; i < 100; i++ {
		slug := generateSlug()
		var exists int
		s.db.QueryRow("SELECT COUNT(*) FROM transcripts WHERE slug = ?", slug).Scan(&exists)
		if exists == 0 {
			return slug
		}
	}
	// Fallback: append timestamp
	return generateSlug() + "-" + fmt.Sprintf("%d", time.Now().UnixMilli()%10000)
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SaveTranscript(audioPath, title string) (int64, error) {
	slug := s.uniqueSlug()
	res, err := s.db.Exec("INSERT INTO transcripts (slug, audio_path, title) VALUES (?, ?, ?)", slug, audioPath, title)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateTranscript(id int64, audioPath, title string) error {
	_, err := s.db.Exec("UPDATE transcripts SET audio_path = ?, title = ? WHERE id = ?", audioPath, title, id)
	return err
}

func (s *Store) UpdateTitle(id int64, title string) error {
	_, err := s.db.Exec("UPDATE transcripts SET title = ? WHERE id = ?", title, id)
	return err
}

func (s *Store) SetSourceURL(id int64, sourceURL string) error {
	_, err := s.db.Exec("UPDATE transcripts SET source_url = ? WHERE id = ?", sourceURL, id)
	return err
}

func (s *Store) SaveClaim(text, claimType string) (int64, error) {
	hash := NormHash(text)
	// Try insert, on conflict return existing
	res, err := s.db.Exec("INSERT OR IGNORE INTO claims (text, type, norm_hash) VALUES (?, ?, ?)", text, claimType, hash)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id > 0 {
		return id, nil
	}
	// Already exists
	var existing int64
	err = s.db.QueryRow("SELECT id FROM claims WHERE norm_hash = ?", hash).Scan(&existing)
	return existing, err
}

func (s *Store) SaveOccurrence(claimID, transcriptID int64, speaker string, position int, originalText string, msgIndex *int) error {
	_, err := s.db.Exec("INSERT INTO occurrences (claim_id, transcript_id, speaker, position, original_text, msg_index) VALUES (?, ?, ?, ?, ?, ?)",
		claimID, transcriptID, speaker, position, originalText, msgIndex)
	return err
}

func (s *Store) SaveEdge(parentID, childID int64, relation string, transcriptID int64) error {
	_, err := s.db.Exec("INSERT INTO edges (parent_id, child_id, relation, transcript_id) VALUES (?, ?, ?, ?)",
		parentID, childID, relation, transcriptID)
	return err
}

func (s *Store) SaveDiarization(transcriptID int64, speakers map[string]string, messages []DiarizeMessage) error {
	// Clear existing utterances for this transcript
	_, err := s.db.Exec("DELETE FROM utterances WHERE transcript_id = ?", transcriptID)
	if err != nil {
		return err
	}

	// Insert each message as a row
	stmt, err := s.db.Prepare("INSERT INTO utterances (transcript_id, speaker, text, position, start_ms, end_ms) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, msg := range messages {
		pos := i + 1 // 1-based position
		if msg.Position > 0 {
			pos = msg.Position
		}
		_, err := stmt.Exec(transcriptID, msg.Speaker, msg.Text, pos, msg.StartMs, msg.EndMs)
		if err != nil {
			return err
		}
	}

	// Also save to speakers + transcript_speakers tables
	s.SaveSpeakersForTranscript(transcriptID, speakers)
	return nil
}

func (s *Store) GetDiarization(transcriptID int64) (map[string]string, []DiarizeMessage, error) {
	rows, err := s.db.Query("SELECT speaker, text, position, start_ms, end_ms FROM utterances WHERE transcript_id = ? ORDER BY position", transcriptID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var messages []DiarizeMessage
	for rows.Next() {
		var msg DiarizeMessage
		if err := rows.Scan(&msg.Speaker, &msg.Text, &msg.Position, &msg.StartMs, &msg.EndMs); err != nil {
			return nil, nil, err
		}
		messages = append(messages, msg)
	}
	if len(messages) == 0 {
		return nil, nil, nil
	}

	// Build speakers map from transcript_speakers table
	speakers := make(map[string]string)
	tsSpeakers, tsErr := s.GetTranscriptSpeakers(transcriptID)
	if tsErr == nil && len(tsSpeakers) > 0 {
		for localID, sp := range tsSpeakers {
			speakers[localID] = sp.Name
		}
	} else {
		// Fallback: extract unique speakers from utterances
		for _, msg := range messages {
			if _, ok := speakers[msg.Speaker]; !ok {
				speakers[msg.Speaker] = ""
			}
		}
	}

	return speakers, messages, nil
}

type Speaker struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	AutoGenerated bool      `json:"auto_generated"`
	CreatedAt     time.Time `json:"created_at"`
}

var randomFirstNames = []string{
	"Alex", "Blake", "Casey", "Dana", "Eden", "Finn", "Gray", "Harper",
	"Ivy", "Jay", "Kit", "Lane", "Morgan", "Noel", "Oak", "Parker",
	"Quinn", "Ray", "Sam", "Tate", "Val", "Wren", "Zara", "Sage",
	"Ash", "Brook", "Drew", "Ellis", "Fern", "Glen", "Haven", "Jade",
	"Kai", "Lark", "Maple", "Nico", "Olive", "Pax", "Reed", "Sky",
	"Thorn", "Uma", "Vex", "Wade", "Xen", "Yuri", "Zoe", "Birch",
	"Cove", "Dune",
}

func GenerateRandomName() string {
	return randomFirstNames[rand.Intn(len(randomFirstNames))]
}

func (s *Store) GenerateUniqueName() string {
	for attempts := 0; attempts < 100; attempts++ {
		name := GenerateRandomName()
		var count int
		s.db.QueryRow("SELECT COUNT(*) FROM speakers WHERE name = ?", name).Scan(&count)
		if count == 0 {
			return name
		}
	}
	// Fallback with number
	return fmt.Sprintf("%s%d", GenerateRandomName(), rand.Intn(99))
}

type SpeakerSummary struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	ConversationCount int  `json:"conversation_count"`
	ClaimCount      int    `json:"claim_count"`
}

type SpeakerConversation struct {
	TranscriptID int64     `json:"transcript_id"`
	Slug         string    `json:"slug"`
	Title        string    `json:"title"`
	CreatedAt    time.Time `json:"created_at"`
	ClaimCount   int       `json:"claim_count"`
}

// GetOrCreateSpeaker finds a speaker by name or creates one.
func (s *Store) GetOrCreateSpeaker(name string, autoGenerated bool) (int64, error) {
	if name == "" {
		return 0, fmt.Errorf("empty speaker name")
	}
	ag := 0
	if autoGenerated {
		ag = 1
	}
	s.db.Exec("INSERT OR IGNORE INTO speakers (name, auto_generated) VALUES (?, ?)", name, ag)
	var id int64
	err := s.db.QueryRow("SELECT id FROM speakers WHERE name = ?", name).Scan(&id)
	return id, err
}

// LinkSpeakerToTranscript associates a speaker with a transcript's local ID.
func (s *Store) LinkSpeakerToTranscript(transcriptID, speakerID int64, localID string) error {
	_, err := s.db.Exec(
		"INSERT OR REPLACE INTO transcript_speakers (transcript_id, speaker_id, local_id) VALUES (?, ?, ?)",
		transcriptID, speakerID, localID)
	return err
}

// RenameSpeaker updates a speaker's canonical name globally.
func (s *Store) RenameSpeaker(id int64, newName string) error {
	if newName == "" {
		return fmt.Errorf("empty name")
	}
	// Check if target name already exists (merge case)
	var existingID int64
	err := s.db.QueryRow("SELECT id FROM speakers WHERE name = ? AND id != ?", newName, id).Scan(&existingID)
	if err == nil {
		// Merge: reassign this speaker's transcripts to the existing speaker,
		// then drop it. Both statements must land together — the reassignment
		// is what leaves the speaker row unreferenced and therefore deletable,
		// so a partial merge would either strand the rows or fail the delete.
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin speaker merge: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.Exec("UPDATE transcript_speakers SET speaker_id = ? WHERE speaker_id = ?", existingID, id); err != nil {
			return fmt.Errorf("reassign transcripts from speaker %d to %d: %w", id, existingID, err)
		}
		if _, err := tx.Exec("DELETE FROM speakers WHERE id = ?", id); err != nil {
			return fmt.Errorf("delete merged speaker %d: %w", id, err)
		}
		return tx.Commit()
	}
	_, err = s.db.Exec("UPDATE speakers SET name = ?, auto_generated = 0 WHERE id = ?", newName, id)
	return err
}

// GetSpeaker returns a speaker by ID.
func (s *Store) GetSpeaker(id int64) (*Speaker, error) {
	sp := &Speaker{}
	var ag int
	err := s.db.QueryRow("SELECT id, name, auto_generated, created_at FROM speakers WHERE id = ?", id).
		Scan(&sp.ID, &sp.Name, &ag, &sp.CreatedAt)
	if err != nil {
		return nil, err
	}
	sp.AutoGenerated = ag != 0
	return sp, nil
}

// GetSpeakerByName returns a speaker by name.
func (s *Store) GetSpeakerByName(name string) (*Speaker, error) {
	sp := &Speaker{}
	var ag int
	err := s.db.QueryRow("SELECT id, name, auto_generated, created_at FROM speakers WHERE name = ?", name).
		Scan(&sp.ID, &sp.Name, &ag, &sp.CreatedAt)
	if err != nil {
		return nil, err
	}
	sp.AutoGenerated = ag != 0
	return sp, nil
}

// GetTranscriptSpeakers returns the speaker mappings for a transcript (local_id → speaker).
func (s *Store) GetTranscriptSpeakers(transcriptID int64) (map[string]*Speaker, error) {
	rows, err := s.db.Query(`
		SELECT ts.local_id, sp.id, sp.name, sp.auto_generated, sp.created_at
		FROM transcript_speakers ts JOIN speakers sp ON ts.speaker_id = sp.id
		WHERE ts.transcript_id = ?`, transcriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]*Speaker{}
	for rows.Next() {
		var localID string
		var ag int
		sp := &Speaker{}
		rows.Scan(&localID, &sp.ID, &sp.Name, &ag, &sp.CreatedAt)
		sp.AutoGenerated = ag != 0
		result[localID] = sp
	}
	return result, nil
}

// SaveSpeakersWithFlags creates/links speakers using explicit auto_generated flags from the frontend.
func (s *Store) SaveSpeakersWithFlags(transcriptID int64, speakers map[string]string, autoGen map[string]bool) error {
	for localID, name := range speakers {
		if name == "" {
			continue
		}
		isAuto := autoGen[localID]
		spID, err := s.GetOrCreateSpeaker(name, isAuto)
		if err != nil {
			continue
		}
		// Ensure auto_generated flag matches what the frontend says
		ag := 0
		if isAuto {
			ag = 1
		}
		s.db.Exec("UPDATE speakers SET auto_generated = ? WHERE id = ? AND auto_generated != ?", ag, spID, ag)
		s.LinkSpeakerToTranscript(transcriptID, spID, localID)
	}
	return nil
}

// SaveSpeakersForTranscript creates/links speakers for a transcript from a name map.
// Names matching "Speaker N" pattern get replaced with random names and marked auto_generated.
func (s *Store) SaveSpeakersForTranscript(transcriptID int64, speakers map[string]string) error {
	defaultPattern := regexp.MustCompile(`(?i)^speaker[_ ]\d+$`)
	for localID, name := range speakers {
		if name == "" {
			continue
		}
		// Check if already linked — don't overwrite existing speaker with auto name
		var existing int64
		err := s.db.QueryRow("SELECT speaker_id FROM transcript_speakers WHERE transcript_id = ? AND local_id = ?",
			transcriptID, localID).Scan(&existing)
		if err == nil {
			// Already linked, skip unless name changed to something non-default
			if !defaultPattern.MatchString(name) {
				spID, err := s.GetOrCreateSpeaker(name, false)
				if err == nil {
					s.LinkSpeakerToTranscript(transcriptID, spID, localID)
				}
			}
			continue
		}

		autoGen := defaultPattern.MatchString(name)
		if autoGen {
			name = s.GenerateUniqueName()
		}
		spID, err := s.GetOrCreateSpeaker(name, autoGen)
		if err != nil {
			continue
		}
		s.LinkSpeakerToTranscript(transcriptID, spID, localID)
	}
	return nil
}

// ListSpeakers returns all speakers with conversation and claim counts.
func (s *Store) ListSpeakers() ([]SpeakerSummary, error) {
	rows, err := s.db.Query(`
		SELECT sp.id, sp.name,
			COUNT(DISTINCT ts.transcript_id) as convo_count,
			COALESCE(SUM(occ_counts.cnt), 0) as claim_count
		FROM speakers sp
		JOIN transcript_speakers ts ON sp.id = ts.speaker_id
		LEFT JOIN (
			SELECT transcript_id, speaker as local_id, COUNT(*) as cnt
			FROM occurrences GROUP BY transcript_id, speaker
		) occ_counts ON ts.transcript_id = occ_counts.transcript_id AND ts.local_id = occ_counts.local_id
		WHERE sp.auto_generated = 0
		GROUP BY sp.id, sp.name
		ORDER BY convo_count DESC, claim_count DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SpeakerSummary
	for rows.Next() {
		var s SpeakerSummary
		rows.Scan(&s.ID, &s.Name, &s.ConversationCount, &s.ClaimCount)
		result = append(result, s)
	}
	return result, nil
}

// GetSpeakerConversations returns all conversations a speaker appears in.
func (s *Store) GetSpeakerConversations(name string) ([]SpeakerConversation, error) {
	rows, err := s.db.Query(`
		SELECT t.id, COALESCE(t.slug,''), COALESCE(t.title,''), t.created_at,
			COALESCE(occ.cnt, 0) as claim_count
		FROM transcript_speakers ts
		JOIN speakers sp ON ts.speaker_id = sp.id
		JOIN transcripts t ON ts.transcript_id = t.id
		LEFT JOIN (
			SELECT o.transcript_id, o.speaker as local_id, COUNT(*) as cnt
			FROM occurrences o GROUP BY o.transcript_id, o.speaker
		) occ ON ts.transcript_id = occ.transcript_id AND ts.local_id = occ.local_id
		WHERE sp.name = ?
		ORDER BY t.created_at DESC`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []SpeakerConversation
	for rows.Next() {
		var sc SpeakerConversation
		rows.Scan(&sc.TranscriptID, &sc.Slug, &sc.Title, &sc.CreatedAt, &sc.ClaimCount)
		result = append(result, sc)
	}
	return result, nil
}

func (s *Store) GetTranscript(id int64) (*Transcript, error) {
	t := &Transcript{}
	err := s.db.QueryRow("SELECT id, COALESCE(slug,''), audio_path, title, COALESCE(source_url,''), created_at FROM transcripts WHERE id = ?", id).
		Scan(&t.ID, &t.Slug, &t.AudioPath, &t.Title, &t.SourceURL, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) GetTranscriptBySlug(slug string) (*Transcript, error) {
	t := &Transcript{}
	err := s.db.QueryRow("SELECT id, COALESCE(slug,''), audio_path, title, COALESCE(source_url,''), created_at FROM transcripts WHERE slug = ?", slug).
		Scan(&t.ID, &t.Slug, &t.AudioPath, &t.Title, &t.SourceURL, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) ListTranscripts() ([]Transcript, error) {
	rows, err := s.db.Query("SELECT id, COALESCE(slug,''), audio_path, title, COALESCE(source_url,''), created_at FROM transcripts ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Transcript
	for rows.Next() {
		var t Transcript
		if err := rows.Scan(&t.ID, &t.Slug, &t.AudioPath, &t.Title, &t.SourceURL, &t.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (s *Store) GetClaim(id int64) (*Claim, error) {
	c := &Claim{}
	err := s.db.QueryRow("SELECT id, text, type, norm_hash, created_at FROM claims WHERE id = ?", id).
		Scan(&c.ID, &c.Text, &c.Type, &c.NormHash, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) GetClaimByHash(hash string) (*Claim, error) {
	c := &Claim{}
	err := s.db.QueryRow("SELECT id, text, type, norm_hash, created_at FROM claims WHERE norm_hash = ?", hash).
		Scan(&c.ID, &c.Text, &c.Type, &c.NormHash, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) GetClaimTree(transcriptID int64) ([]ClaimTreeNode, error) {
	// Get all claims for this transcript via occurrences
	type claimOcc struct {
		ClaimID      int64
		Text         string
		Type         string
		Speaker      string
		Position     int
		OriginalText string
		MsgIndex     *int
	}
	rows, err := s.db.Query(`
		SELECT c.id, c.text, c.type, o.speaker, o.position, o.original_text, o.msg_index
		FROM occurrences o JOIN claims c ON o.claim_id = c.id
		WHERE o.transcript_id = ? ORDER BY o.position`, transcriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	claims := map[int64]*claimOcc{}
	var order []int64
	for rows.Next() {
		co := &claimOcc{}
		if err := rows.Scan(&co.ClaimID, &co.Text, &co.Type, &co.Speaker, &co.Position, &co.OriginalText, &co.MsgIndex); err != nil {
			return nil, err
		}
		claims[co.ClaimID] = co
		order = append(order, co.ClaimID)
	}

	// Get edges for this transcript
	edgeRows, err := s.db.Query("SELECT parent_id, child_id FROM edges WHERE transcript_id = ?", transcriptID)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()

	childToParent := map[int64]int64{}
	parentToChildren := map[int64][]int64{}
	for edgeRows.Next() {
		var pid, cid int64
		edgeRows.Scan(&pid, &cid)
		childToParent[cid] = pid
		parentToChildren[pid] = append(parentToChildren[pid], cid)
	}

	// Build tree (with cycle detection)
	var buildNode func(id int64, visited map[int64]bool) ClaimTreeNode
	buildNode = func(id int64, visited map[int64]bool) ClaimTreeNode {
		co := claims[id]
		node := ClaimTreeNode{
			Claim:        Claim{ID: co.ClaimID, Text: co.Text, Type: co.Type},
			Speaker:      co.Speaker,
			OriginalText: co.OriginalText,
			MsgIndex:     co.MsgIndex,
		}
		visited[id] = true
		for _, childID := range parentToChildren[id] {
			if !visited[childID] {
				node.Children = append(node.Children, buildNode(childID, visited))
			}
		}
		return node
	}

	var roots []ClaimTreeNode
	for _, id := range order {
		if _, hasParent := childToParent[id]; !hasParent {
			roots = append(roots, buildNode(id, map[int64]bool{}))
		}
	}
	return roots, nil
}

func (s *Store) GetClaimGraph(claimID int64) (*ClaimWithDetails, error) {
	claim, err := s.GetClaim(claimID)
	if err != nil {
		return nil, err
	}
	result := &ClaimWithDetails{Claim: *claim}

	// Occurrences
	rows, err := s.db.Query(`
		SELECT o.id, o.claim_id, o.transcript_id, o.speaker, o.position, o.original_text, COALESCE(t.title,'')
		FROM occurrences o LEFT JOIN transcripts t ON o.transcript_id = t.id
		WHERE o.claim_id = ?`, claimID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o OccurrenceWithTranscript
		rows.Scan(&o.ID, &o.ClaimID, &o.TranscriptID, &o.Speaker, &o.Position, &o.OriginalText, &o.TranscriptTitle)
		result.Occurrences = append(result.Occurrences, o)
	}

	// Edges where this claim is parent or child
	edgeRows, err := s.db.Query(`
		SELECT e.id, e.parent_id, e.child_id, e.relation, e.transcript_id,
			COALESCE(p.text,''), COALESCE(c.text,'')
		FROM edges e
		LEFT JOIN claims p ON e.parent_id = p.id
		LEFT JOIN claims c ON e.child_id = c.id
		WHERE e.parent_id = ? OR e.child_id = ?`, claimID, claimID)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var e EdgeWithClaims
		edgeRows.Scan(&e.ID, &e.ParentID, &e.ChildID, &e.Relation, &e.TranscriptID, &e.ParentText, &e.ChildText)
		result.Edges = append(result.Edges, e)
	}

	return result, nil
}

func (s *Store) GetFullGraph() (*GraphData, error) {
	claimRows, err := s.db.Query("SELECT id, text, type, norm_hash, created_at FROM claims")
	if err != nil {
		return nil, err
	}
	defer claimRows.Close()

	g := &GraphData{}
	for claimRows.Next() {
		var c Claim
		claimRows.Scan(&c.ID, &c.Text, &c.Type, &c.NormHash, &c.CreatedAt)
		g.Claims = append(g.Claims, c)
	}

	edgeRows, err := s.db.Query("SELECT id, parent_id, child_id, relation, transcript_id FROM edges")
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()
	for edgeRows.Next() {
		var e Edge
		edgeRows.Scan(&e.ID, &e.ParentID, &e.ChildID, &e.Relation, &e.TranscriptID)
		g.Edges = append(g.Edges, e)
	}

	return g, nil
}
