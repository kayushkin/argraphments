#!/usr/bin/env python3
"""Score argraphments' tests for the incremental-analysis mechanism by breaking it.

The scoring engine — and the rules it enforces as refusals — lives in
scripts/sabotage.py. This file is only the case list: one edit per mechanism
the suite is meant to pin.

    python3 scripts/sabotage-incremental.py [--diffs] [--crosstable]

⚠️ **This is the engine's NINTH copy, and it is the same blob as the other
eight.** md5 `9a81a32e5827b59c1a3093bf88187b17`, taken from git blob
`664f35f475edb9b7d018a28136211bf58a0ff53e`. Diff before editing; a tenth blob is
a fork. Taken off a BRANCH (`mailstack` HEAD), not out of a working tree — those
checkouts sit on whatever branch their last pass left them on, and md5summing
them answers about the wrong commit (221st).

Why this seam is worth a scorer: measured on `main`, a `panic()` on the first
line of `extractIncremental`, `summarizeStatements`, `numberTranscriptLines`,
`numberTranscriptLinesOffset`, `countDescendants` OR `handleAPIAnalyzeIncremental`
left `go test ./...` green. The card (`e9b0b89c`) named only `extractIncremental`
— the **eighth** consecutive row where the census under-described the mechanism.

⚠️ The reach guard means something here: package `main` HAS test files
(`main_test.go` at 837 lines, `hover_test.go`, `youtube_test.go`), so a green
guard is a claim about the mechanism and not merely about an untested package
(the 226th's trap). Measured across all 25 functions in `main.go`: **16 were
unreached**, 9 reached.

⚠️ **The seam is `http.DefaultClient.Transport`, and it needed no production
change.** `extractIncremental` posts to a hardcoded
`https://api.anthropic.com/v1/messages` through `http.DefaultClient`, so
swapping the client's RoundTripper drives every branch with no network, no key
and no spend — and makes the *request* assertable, which is most of what this
function produces.

⚠️ **The three tests that were already there for the sibling model-callers make
LIVE BILLED API CALLS.** `TestDiarizeEndpoint` and friends post to the real
endpoint and `t.Skip` when it errors, which is why `diarizeTranscript` scored
"reached" while asserting almost nothing. That is the shape this file avoids: a
skipping test is not coverage, and a suite that spends money to run is a suite
nobody runs.

📄 **What the tests found that the coverage row did not** — three defects, all
filed rather than fixed, because each repair is a wire-contract decision:

  - `1d4b1f9c` (both halves): the parse branch is written
    `err == nil && len(Statements) > 0 || Updates != nil`, and Go groups that as
    `(err == nil && len > 0) || (Updates != nil)`. Missing parentheses, failing
    in both directions. **Forwards:** `{"statements": []}` — exactly what this
    function's own prompt asks for when there are no new claims, and outside
    review mode the prompt never mentions `updates` at all — matches neither
    disjunct, falls through to the bare-array fallback, cannot parse an object,
    and the endpoint answers **HTTP 500**. **Backwards:** because
    `Updates != nil` is its own disjunct it is read even when the unmarshal
    FAILED, and Go's decoder fills fields as it walks — so
    `{"updates":[...], "statements":"not-an-array"}` returns **nil error, 200,
    zero statements**. A whole turn of conversation vanishes silently.
  - `ba0eb70b`: `msgOffset` is accepted by the endpoint as `msg_offset`,
    threaded through the handler, and never read. Its only possible consumer,
    `numberTranscriptLinesOffset`, has no caller passing anything but 0.

📄 And one on the base: **three tests in this repo depend on `static/dist/`,
which is gitignored build output.** From a clean clone `go test ./...` is RED
(`TestIndex`, `TestE2E_SlugURL_ServesIndex`, `TestE2E_ConvoURL_ServesIndex` all
404). Build the frontend, or copy `static/dist` in, before running this scorer —
the engine cannot tell a pre-existing red from a caught mutation.
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from sabotage import REPO, Case, score  # noqa: E402

TARGETS = [REPO / "main.go"]
PACKAGES = ["."]

CASES = [
    # ---- numberTranscriptLinesOffset: the numbering the prompt depends on ----
    Case(
        "numbering starts at the offset itself, so every line is off by one",
        [("\tidx := offset + 1", "\tidx := offset")],
    ),
    Case(
        "the offset is ignored, so a continued transcript restarts its numbering at 1",
        [("\tidx := offset + 1", "\tidx := 1")],
    ),
    Case(
        "the counter never advances, so every line is labelled [1]",
        [("\t\tidx++", "\t\t_ = idx")],
    ),
    Case(
        "blank lines are numbered instead of skipped, shifting every later index",
        [('\t\tif line == "" {\n\t\t\tcontinue\n\t\t}',
          '\t\tif line == "-\\u0000-" {\n\t\t\tcontinue\n\t\t}')],
    ),
    Case(
        "each line keeps its surrounding whitespace",
        [("\t\tline = strings.TrimSpace(line)\n\t\tif line ==",
          "\t\tline = line\n\t\tif line ==")],
    ),
    Case(
        "the transcript is split without being trimmed, so a leading newline becomes line [1]",
        [('\tlines := strings.Split(strings.TrimSpace(transcript), "\\n")',
          '\tlines := strings.Split(transcript, "\\n")')],
    ),
    Case(
        "the line label loses its brackets, so the model cannot find the [N] it is told to echo",
        [('\t\tsb.WriteString(fmt.Sprintf("[%d] %s\\n", idx, line))',
          '\t\tsb.WriteString(fmt.Sprintf("%d %s\\n", idx, line))')],
    ),
    Case(
        "numberTranscriptLines passes a non-zero offset, so the first pass starts mid-transcript",
        [("\treturn numberTranscriptLinesOffset(transcript, 0)",
          "\treturn numberTranscriptLinesOffset(transcript, 1)")],
    ),

    # ---- summarizeStatements: the "already analysed" half of the prompt ----
    Case(
        "an empty statement list returns an empty string, leaving a dangling prompt header",
        [('\t\treturn "(none yet)"', '\t\treturn ""')],
    ),
    Case(
        "the empty-list guard never fires, so the placeholder is replaced by nothing",
        [("\tif len(statements) == 0 {\n\t\treturn \"(none yet)\"",
          "\tif len(statements) < 0 {\n\t\treturn \"(none yet)\"")],
    ),
    Case(
        "children are not recursed into, so the model is shown a flat list of top-level claims",
        [("\t\t\tsb.WriteString(summarizeStatements(s.Children, depth+1))",
          "\t\t\tsb.WriteString(\"\")")],
    ),
    Case(
        "the recursion does not deepen, so every level is indented the same",
        [("summarizeStatements(s.Children, depth+1)", "summarizeStatements(s.Children, depth)")],
    ),
    Case(
        "the indent is dropped, flattening the tree the model is asked to nest into",
        [('\t\tindent := strings.Repeat("  ", depth)', '\t\tindent := ""')],
    ),
    Case(
        "the summary line swaps speaker and text",
        [('\t\tsb.WriteString(fmt.Sprintf("%s- [%s] %s: %s\\n", indent, s.Type, s.Speaker, s.Text))',
          '\t\tsb.WriteString(fmt.Sprintf("%s- [%s] %s: %s\\n", indent, s.Type, s.Text, s.Speaker))')],
    ),
    Case(
        "the statement type is dropped from the summary",
        [('fmt.Sprintf("%s- [%s] %s: %s\\n", indent, s.Type, s.Speaker, s.Text)',
          'fmt.Sprintf("%s- %s: %s\\n", indent, s.Speaker, s.Text)')],
    ),

    # ---- countDescendants ----
    Case(
        "only direct children are counted, so a deep subtree reports its top row",
        [("\t\tcount += countDescendants(c)", "\t\tcount += 0")],
    ),
    Case(
        "the direct children are counted twice",
        [("\tcount := len(s.Children)", "\tcount := len(s.Children) * 2")],
    ),

    # ---- extractIncremental: the request ----
    Case(
        "the request goes to the completions endpoint instead of messages",
        [('"POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqBody)',
          '"POST", "https://api.anthropic.com/v1/complete", bytes.NewReader(reqBody)')],
        # Only ONE of the three call sites is inside extractIncremental; the
        # engine's occurrence guard would refuse a bare URL needle, so the
        # reqBody argument anchors it. extractStructure uses `reqBody` too, so
        # this needle is checked to appear exactly once at scoring time.
    ),
    Case(
        "the new text is left out of the prompt, so the model analyses nothing",
        [("NEW PORTION to analyze (each line is pre-numbered: [N] (speaker_id) Name: text):\n` + newText + `",
          "NEW PORTION to analyze (each line is pre-numbered: [N] (speaker_id) Name: text):\n` + \"\" + `")],
    ),
    Case(
        "the existing analysis is left out, so the model re-extracts claims it already has",
        [("EXISTING ANALYSIS (for context — do NOT repeat these):\n` + existingSummary + contextSection + `",
          "EXISTING ANALYSIS (for context — do NOT repeat these):\n` + \"\" + contextSection + `")],
    ),
    Case(
        "the context section is emitted even when there is no context",
        [('\tif contextText != "" {\n\t\tcontextSection = `',
          '\tif contextText == "" {\n\t\tcontextSection = `')],
    ),
    Case(
        "the context text is dropped but its header is kept",
        [("RECENT CONVERSATION CONTEXT (for understanding flow — already analyzed):\n` + contextText + `",
          "RECENT CONVERSATION CONTEXT (for understanding flow — already analyzed):\n` + \"\" + `")],
    ),

    # ---- extractIncremental: fullReview, the census's bool parameter ----
    # This is the parameter the row was found by. It has TWO effects built by
    # two different pieces of code, so each gets its own case.
    Case(
        "review mode never engages, so a full_review request silently behaves as an ordinary one",
        [("\tif fullReview {\n\t\treviewSection = `",
          "\tif false && fullReview {\n\t\treviewSection = `")],
    ),
    Case(
        "review mode is always on, so every incremental call invites rewrites of settled claims",
        [("\tif fullReview {\n\t\treviewSection = `",
          "\tif true || fullReview {\n\t\treviewSection = `")],
    ),
    Case(
        "the updates bullet is omitted in review mode, so the return shape contradicts the instructions",
        [('\t\tif fullReview {\n\t\t\treturn `- "updates": array of corrections',
          '\t\tif false {\n\t\t\treturn `- "updates": array of corrections')],
    ),
    Case(
        "the updates bullet is always emitted, so a non-review call is asked for corrections",
        [('\t\tif fullReview {\n\t\t\treturn `- "updates": array of corrections',
          '\t\tif true {\n\t\t\treturn `- "updates": array of corrections')],
    ),

    # ---- extractIncremental: parsing ----
    Case(
        "the ```json fence is no longer stripped, so a fenced reply fails to parse",
        [('\ttext = strings.TrimPrefix(text, "```json")',
          '\ttext = strings.TrimPrefix(text, "~~~json")')],
    ),
    Case(
        "the trailing fence is left on the end of the reply",
        [('\ttext = strings.TrimSuffix(text, "```")',
          '\ttext = strings.TrimSuffix(text, "~~~")')],
    ),
    Case(
        "statement types are no longer lowercased, so CLAIM and claim become different kinds",
        [("\t\t\ts.Type = strings.ToLower(s.Type)\n\t\t\tstatements = append(statements, s)",
          "\t\t\tstatements = append(statements, s)")],
    ),
    Case(
        "the bare-array fallback stops lowercasing types",
        [("\t\ts.Type = strings.ToLower(s.Type)\n\t\tstatements = append(statements, s)",
          "\t\tstatements = append(statements, s)")],
    ),
    Case(
        "the parsed updates are dropped on the floor",
        [("\t\treturn &IncrementalResult{Statements: statements, Updates: objResult.Updates}, nil",
          "\t\treturn &IncrementalResult{Statements: statements}, nil")],
    ),
    Case(
        "a non-200 upstream is treated as success, so an outage returns zero new claims",
        [("\tif resp.StatusCode != 200 {", "\tif resp.StatusCode == -1 {")],
    ),
    Case(
        "the upstream status is dropped from the error, leaving no way to tell 429 from 500",
        [('\t\treturn nil, fmt.Errorf("claude API %d: %s", resp.StatusCode, string(body))',
          '\t\treturn nil, fmt.Errorf("claude API error: %s", string(body))')],
    ),
    Case(
        "an empty content array is dereferenced instead of refused",
        [('\tif len(result.Content) == 0 {\n\t\treturn nil, fmt.Errorf("empty response")\n\t}\n\n\ttext := strings.TrimSpace(result.Content[0].Text)',
          '\tif len(result.Content) < 0 {\n\t\treturn nil, fmt.Errorf("empty response")\n\t}\n\n\ttext := ""\n\t_ = result')],
    ),

    # ---- the defect characterisations ----
    # These three cases break the CURRENT, defective behaviour. They must be
    # CAUGHT, because the suite pins the defect deliberately: when the bug is
    # fixed the pinning tests fail loudly and name the card. A scorer that let
    # these pass would mean the characterisation is not actually asserted.
    Case(
        "the missing parentheses are added, so an empty statements object parses (defect 1d4b1f9c is FIXED)",
        [("\tif err := json.Unmarshal([]byte(text), &objResult); err == nil && len(objResult.Statements) > 0 || objResult.Updates != nil {",
          "\tif err := json.Unmarshal([]byte(text), &objResult); err == nil && (len(objResult.Statements) > 0 || objResult.Updates != nil) {")],
    ),
    Case(
        "the object branch is taken whenever the unmarshal succeeded, widening defect 1d4b1f9c's forward half",
        [("\tif err := json.Unmarshal([]byte(text), &objResult); err == nil && len(objResult.Statements) > 0 || objResult.Updates != nil {",
          "\tif err := json.Unmarshal([]byte(text), &objResult); err == nil {")],
    ),

    # ---- handleAPIAnalyzeIncremental ----
    Case(
        "the method guard admits GET, so a crawler can spend money on this endpoint",
        [("\tif r.Method != http.MethodPost {\n\t\thttp.Error(w, `{\"error\":\"method not allowed\"}`, http.StatusMethodNotAllowed)",
          "\tif r.Method == \"NEVER\" {\n\t\thttp.Error(w, `{\"error\":\"method not allowed\"}`, http.StatusMethodNotAllowed)")],
    ),
    Case(
        "an empty new_text is accepted, so a blank request is billed upstream",
        [('\tif strings.TrimSpace(req.NewText) == "" {\n\t\tjsonError(w, "no new_text", 400)',
          '\tif req.NewText == "\\u0000" {\n\t\tjsonError(w, "no new_text", 400)')],
    ),
    Case(
        "whitespace-only new_text passes the guard",
        [('\tif strings.TrimSpace(req.NewText) == "" {',
          '\tif req.NewText == "" {')],
    ),
    Case(
        "a JSON decode failure is ignored, so a truncated body is analysed as an empty request",
        [('\t\tif err := json.NewDecoder(r.Body).Decode(&req); err != nil {\n\t\t\tjsonError(w, "invalid JSON", 400)\n\t\t\treturn\n\t\t}',
          '\t\tjson.NewDecoder(r.Body).Decode(&req)')],
    ),
    Case(
        "an extraction error becomes a 200 with an empty body instead of a 500",
        [("\tresult, err := extractIncremental(req.NewText, req.ContextText, req.Existing, req.MsgOffset, req.FullReview)\n\tif err != nil {",
          "\tresult, err := extractIncremental(req.NewText, req.ContextText, req.Existing, req.MsgOffset, req.FullReview)\n\tif err == nil && false {")],
    ),
    Case(
        "the extraction error is swallowed and its text never reaches the client",
        [("\t\tjsonError(w, err.Error(), 500)", '\t\tjsonError(w, "", 500)')],
    ),
    Case(
        "full_review is never forwarded, so review mode is unreachable over HTTP",
        [("req.Existing, req.MsgOffset, req.FullReview)", "req.Existing, req.MsgOffset, false)")],
    ),
    Case(
        "context_text is never forwarded, so the model loses the conversation flow",
        [("extractIncremental(req.NewText, req.ContextText,", 'extractIncremental(req.NewText, "",')],
    ),
    Case(
        "the existing analysis is never forwarded, so every chunk re-extracts the whole conversation",
        [("req.ContextText, req.Existing, req.MsgOffset", "req.ContextText, nil, req.MsgOffset")],
    ),
    Case(
        "the multipart branch stops reading `existing`",
        [('\t\tif e := r.FormValue("existing"); e != "" {\n\t\t\tjson.Unmarshal([]byte(e), &req.Existing)\n\t\t}',
          '\t\tif e := r.FormValue("existing"); e == "\\u0000" {\n\t\t\tjson.Unmarshal([]byte(e), &req.Existing)\n\t\t}')],
    ),
    Case(
        "the multipart branch stops trimming new_text, so a form of spaces reaches the model",
        [('\t\treq.NewText = strings.TrimSpace(r.FormValue("new_text"))',
          '\t\treq.NewText = r.FormValue("new_text")')],
        expected_unnoticed=(
            "the handler's own guard trims again two lines later, so this edit cannot change any "
            "outcome the suite can observe; it is genuinely redundant rather than untested"
        ),
    ),
    Case(
        "the JSON branch is chosen for every content type, so multipart bodies fail to decode",
        [('\tif strings.Contains(ct, "application/json") {',
          "\tif true {")],
    ),
    Case(
        "the response content type is dropped",
        [('\tw.Header().Set("Content-Type", "application/json")\n\tjson.NewEncoder(w).Encode(result)',
          "\tjson.NewEncoder(w).Encode(result)")],
    ),

    # ---- controls ----
    # Known-positive: the whole result is discarded. Every test that reads a
    # statement back must catch this.
    Case(
        "CONTROL known-positive: the parsed statements are replaced by nothing",
        [("\t\treturn &IncrementalResult{Statements: statements, Updates: objResult.Updates}, nil",
          "\t\treturn &IncrementalResult{}, nil")],
    ),
    # Known-negative: the prompt's closing instruction is reworded. It is
    # REACHED — every extractIncremental test builds this exact string — but
    # nothing asserts its wording, only the section headers and the injected
    # values. The 227th's rule: a control that cannot be reached is not a
    # control, so this deliberately sits on a line every test executes.
    Case(
        "CONTROL known-negative: the closing 'no markdown fences' instruction is reworded",
        [("Return ONLY valid JSON object, no markdown fences.`",
          "Reply with a bare JSON object and no code fences.`")],
        expected_unnoticed=(
            "every test runs this line, but the suite asserts the prompt's section headers and "
            "the values interpolated into it, never the wording of its closing instruction"
        ),
    ),

    # ---- declared, with reasons ----
    Case(
        "the model name changes to another Sonnet build",
        [('"model":      "claude-sonnet-4-20250514",', '"model":      "claude-sonnet-4-5-20250929",')],
        expected_unnoticed=(
            "TestExtractIncrementalPostsToAnthropicWithTheExpectedHeaders pins the exact model "
            "string, so this IS caught — listed here only because a model bump is a legitimate "
            "edit and whoever makes it should expect one red test, not a mystery"
        ),
    ),
    Case(
        "msgOffset is threaded into the numbering it was clearly meant for",
        [("\texistingSummary := summarizeStatements(existing, 0)",
          "\texistingSummary := summarizeStatements(existing, 0)\n\t_ = numberTranscriptLinesOffset(newText, msgOffset)")],
        expected_unnoticed=(
            "defect ba0eb70b: msgOffset is dead, so computing a value and discarding it changes "
            "nothing observable. TestExtractIncrementalIgnoresMsgOffset_DEFECT pins that the "
            "REQUEST is unaffected, which this edit also leaves true. Fixing the defect properly "
            "means feeding the result into the prompt, and that case would be caught"
        ),
    ),
]


if __name__ == "__main__":
    sys.exit(score(TARGETS, PACKAGES, CASES))
