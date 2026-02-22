package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := os.Getenv("ARGRAPHMENTS_DB")
	if dbPath == "" {
		dbPath = "./argraphments.db"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Get all transcripts
	transcripts, err := db.Query("SELECT id, slug FROM transcripts")
	if err != nil {
		log.Fatal(err)
	}
	defer transcripts.Close()

	type transcript struct {
		id   int64
		slug string
	}
	var tList []transcript
	for transcripts.Next() {
		var t transcript
		transcripts.Scan(&t.id, &t.slug)
		tList = append(tList, t)
	}

	fmt.Printf("Found %d transcripts to migrate\n", len(tList))

	for _, t := range tList {
		fmt.Printf("\nMigrating %s (ID: %d)...\n", t.slug, t.id)

		// Get speaker mapping for this transcript (speaker_1 -> Name)
		speakerMap := make(map[string]string)
		rows, err := db.Query(`
			SELECT ts.local_id, sp.name
			FROM transcript_speakers ts
			JOIN speakers sp ON ts.speaker_id = sp.id
			WHERE ts.transcript_id = ?
		`, t.id)
		if err != nil {
			log.Printf("  Failed to get speakers: %v", err)
			continue
		}
		for rows.Next() {
			var localID, name string
			rows.Scan(&localID, &name)
			speakerMap[name] = localID
		}
		rows.Close()

		if len(speakerMap) == 0 {
			fmt.Printf("  No speaker mappings found, skipping\n")
			continue
		}

		// Update occurrences to use speaker_id instead of display name
		totalUpdated := 0
		for name, localID := range speakerMap {
			result, err := db.Exec(`
				UPDATE occurrences 
				SET speaker = ?
				WHERE transcript_id = ? AND speaker = ?
			`, localID, t.id, name)
			if err != nil {
				log.Printf("  Failed to update speaker %s: %v", name, err)
				continue
			}
			affected, _ := result.RowsAffected()
			if affected > 0 {
				totalUpdated += int(affected)
				fmt.Printf("  Updated %d occurrences: %s -> %s\n", affected, name, localID)
			}
		}

		if totalUpdated > 0 {
			fmt.Printf("  Total updated: %d occurrences\n", totalUpdated)
		} else {
			fmt.Printf("  No updates needed\n")
		}
	}

	fmt.Println("\nMigration complete!")
}
