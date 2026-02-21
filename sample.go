package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"

	"github.com/kayushkin/argraphments/storage"
)

var sampleYouTubeURLs = []string{
	"https://www.youtube.com/watch?v=x6fIseKzzH0",
	"https://www.youtube.com/watch?v=jPhJbKBuNnA",
	"https://www.youtube.com/watch?v=lzRvSWmMXgY",
	"https://www.youtube.com/watch?v=glM80kRWbes",
	"https://www.youtube.com/watch?v=aeM4jD9Uv_Y",
	"https://www.youtube.com/watch?v=lARpY0nIQx0",
	"https://www.youtube.com/watch?v=d5GecYjy9-Q",
	"https://www.youtube.com/watch?v=J6lyURyVz7k",
	"https://www.youtube.com/watch?v=8gRPsyKU1w0",
	"https://www.youtube.com/watch?v=Hatav_Rdnno",
	"https://www.youtube.com/watch?v=Ut4dphQ9WQc",
	"https://www.youtube.com/watch?v=YlsNIqfgBWc",
	"https://www.youtube.com/watch?v=MPFLGo_vRrQ",
	"https://www.youtube.com/watch?v=HpU4sBm1jJY",
	"https://www.youtube.com/watch?v=yP1MtaSIePk",
	"https://www.youtube.com/watch?v=G_cVpj5ddDU",
	"https://www.youtube.com/watch?v=y3-BX-jN_Ac",
	"https://www.youtube.com/watch?v=n5GJMgRwKYY",
	"https://www.youtube.com/watch?v=wTblbYqQQag",
	"https://www.youtube.com/watch?v=yOjSMKMXpCA",
}

func generateConversation(title string) (map[string]string, []storage.DiarizeMessage, error) {
	prompt := fmt.Sprintf(`Generate a realistic 10-14 message debate conversation between exactly two people about this topic: "%s"

Rules:
- Two speakers with short first names (different from each other)
- They should disagree but engage thoughtfully
- Include claims, rebuttals, evidence, and at least one point of agreement
- Keep each message 1-3 sentences
- Make it feel natural, not scripted

Return JSON:
{
  "speakers": {"speaker_1": "Name1", "speaker_2": "Name2"},
  "messages": [
    {"speaker": "speaker_1", "text": "what they said"},
    {"speaker": "speaker_2", "text": "what they said"}
  ]
}

Return ONLY valid JSON, no markdown fences.`, title)

	reqBody, _ := json.Marshal(map[string]any{
		"model":      "claude-sonnet-4-20250514",
		"max_tokens": 2048,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("x-api-key", anthropicKey)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("claude API %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, nil, err
	}
	if len(result.Content) == 0 {
		return nil, nil, fmt.Errorf("empty response")
	}

	text := strings.TrimSpace(result.Content[0].Text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var convo struct {
		Speakers map[string]string        `json:"speakers"`
		Messages []storage.DiarizeMessage `json:"messages"`
	}
	if err := json.Unmarshal([]byte(text), &convo); err != nil {
		return nil, nil, fmt.Errorf("failed to parse conversation: %v", err)
	}

	return convo.Speakers, convo.Messages, nil
}

func handleAPISample(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Pick a random YouTube URL for title context
	url := sampleYouTubeURLs[rand.Intn(len(sampleYouTubeURLs))]

	// Try to fetch title from YouTube
	_, title, _, err := fetchYouTubeTranscript(url)
	if err != nil || title == "" {
		title = "an interesting debate topic"
	}

	// Generate a fake conversation about the topic
	speakers, messages, err := generateConversation(title)
	if err != nil {
		jsonError(w, fmt.Sprintf("generation failed: %v", err), 500)
		return
	}

	// Generate timestamps from word count (~150 wpm = 2.5 words/sec)
	var runningMs int64
	for i := range messages {
		start := runningMs
		messages[i].StartMs = &start
		words := len(strings.Fields(messages[i].Text))
		durationMs := int64(float64(words) / 2.5 * 1000)
		if durationMs < 2000 {
			durationMs = 2000
		}
		// Add slight random variation (±20%)
		jitter := durationMs / 5
		durationMs += int64(rand.Intn(int(jitter*2+1))) - jitter
		end := start + durationMs
		messages[i].EndMs = &end
		// Pause between speakers: 300-1500ms
		pause := int64(300 + rand.Intn(1200))
		runningMs = end + pause
	}

	// Build transcript text
	var sb strings.Builder
	for _, msg := range messages {
		name := speakers[msg.Speaker]
		if name == "" {
			name = msg.Speaker
		}
		sb.WriteString(name + ": " + msg.Text + "\n")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"speakers": speakers,
		"messages": messages,
		"text":     sb.String(),
		"title":    title,
		"url":      url,
	})
}
