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

  - `7fdc428e` (both halves): the parse branch is written
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
  - `7a0c4490`: `msgOffset` is accepted by the endpoint as `msg_offset`,
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

# ⚠️ **main.go holds THREE model-callers whose request/response boilerplate is
# byte-identical** — `extractStructure`, `extractIncremental` and
# `diarizeTranscript` each carry the same twelve lines of `json.Marshal` +
# `http.NewRequest` + header sets + status check, copy-pasted. So every obvious
# needle in that region (`"model": ...`, the endpoint URL, `if resp.StatusCode
# != 200 {`, the fence-stripping lines) appears 3 or 4 times, and the engine's
# occurrence guard refuses all of them. It is right to: sabotaging the first
# occurrence would score `extractStructure` while the row read as a verdict on
# `extractIncremental`.
#
# The 226th met the mild version of this ("an HTTP status write is among the
# worst needles available — anchor on the line above it"). Here the line above
# is duplicated too, and so is the line above that. The ONLY unique text in
# extractIncremental's tail is its own prompt, so the two blocks below start at
# the prompt's closing line and at the `"empty response"` message that differs
# from extractStructure's `"empty response from Claude"`. Cases edit a copy of
# the block rather than a bare line.
#
# > **When a file holds several copy-pasted callers, the anchor is not the line
# > above — it is the nearest text that is about YOUR caller and no other. Work
# > outward from the payload, not upward from the target.**

REQUEST_AND_STATUS = '''Return ONLY valid JSON object, no markdown fences.`

\treqBody, _ := json.Marshal(map[string]any{
\t\t"model":      "claude-sonnet-4-20250514",
\t\t"max_tokens": 4096,
\t\t"messages": []map[string]string{
\t\t\t{"role": "user", "content": prompt},
\t\t},
\t})

\treq, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(reqBody))
\tif err != nil {
\t\treturn nil, err
\t}
\treq.Header.Set("x-api-key", anthropicKey)
\treq.Header.Set("content-type", "application/json")
\treq.Header.Set("anthropic-version", "2023-06-01")

\tresp, err := http.DefaultClient.Do(req)
\tif err != nil {
\t\treturn nil, err
\t}
\tdefer resp.Body.Close()

\tbody, _ := io.ReadAll(resp.Body)
\tif resp.StatusCode != 200 {
\t\treturn nil, fmt.Errorf("claude API %d: %s", resp.StatusCode, string(body))
\t}
'''

PARSE_TAIL = '''\t\treturn nil, fmt.Errorf("empty response")
\t}

\ttext := strings.TrimSpace(result.Content[0].Text)
\ttext = strings.TrimPrefix(text, "```json")
\ttext = strings.TrimPrefix(text, "```")
\ttext = strings.TrimSuffix(text, "```")
\ttext = strings.TrimSpace(text)
'''


def in_request(old, new):
    """One edit inside extractIncremental's copy of the shared request block."""
    assert old in REQUEST_AND_STATUS, old
    return [(REQUEST_AND_STATUS, REQUEST_AND_STATUS.replace(old, new, 1))]


def in_parse(old, new):
    """One edit inside extractIncremental's copy of the shared parse tail."""
    assert old in PARSE_TAIL, old
    return [(PARSE_TAIL, PARSE_TAIL.replace(old, new, 1))]


# The same duplication problem in the HTTP layer: four handlers open with the
# identical POST guard and three decode their body the identical way. The
# function signature is the only unique text at the top, and the
# `extractIncremental` call is the only unique text at the bottom.

HANDLER_HEAD = '''func handleAPIAnalyzeIncremental(w http.ResponseWriter, r *http.Request) {
\tif r.Method != http.MethodPost {
\t\thttp.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
\t\treturn
\t}

\tvar req struct {
\t\tNewText     string      `json:"new_text"`
\t\tContextText string      `json:"context_text"`
\t\tExisting    []Statement `json:"existing"`
\t\tMsgOffset   int         `json:"msg_offset"`
\t\tFullReview  bool        `json:"full_review"`
\t}

\tct := r.Header.Get("Content-Type")
\tif strings.Contains(ct, "application/json") {
\t\tif err := json.NewDecoder(r.Body).Decode(&req); err != nil {
\t\t\tjsonError(w, "invalid JSON", 400)
\t\t\treturn
\t\t}
'''

HANDLER_TAIL = '''\tresult, err := extractIncremental(req.NewText, req.ContextText, req.Existing, req.MsgOffset, req.FullReview)
\tif err != nil {
\t\tjsonError(w, err.Error(), 500)
\t\treturn
\t}

\tw.Header().Set("Content-Type", "application/json")
\tjson.NewEncoder(w).Encode(result)
'''


def in_handler_head(old, new):
    assert old in HANDLER_HEAD, old
    return [(HANDLER_HEAD, HANDLER_HEAD.replace(old, new, 1))]


def in_handler_tail(old, new):
    assert old in HANDLER_TAIL, old
    return [(HANDLER_TAIL, HANDLER_TAIL.replace(old, new, 1))]


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
    # ⚠️ Scored UNNOTICED on the first run, and the tests are not at fault —
    # this guard is unconditionally redundant. Every line is trimmed
    # individually two lines later and blank lines are skipped, so the outer
    # TrimSpace can only ever remove whitespace that the inner pass removes
    # again. Verified by enumeration rather than argued: there is no input for
    # which the two forms differ. Unlike the 228th's `filePath == ""` — which
    # was redundant under the DEFAULT patterns and load-bearing under `["*"]` —
    # this one has no configuration that makes it matter, so it is declared
    # rather than closed.
    Case(
        "the transcript is split without being trimmed first",
        [('\tlines := strings.Split(strings.TrimSpace(transcript), "\\n")',
          '\tlines := strings.Split(transcript, "\\n")')],
        expected_unnoticed=(
            "unconditionally redundant: each line is TrimSpace'd individually and empty lines "
            "are skipped, so no input distinguishes the two forms. Not a coverage gap"
        ),
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
    # ⚠️ The obvious edit here — `count += 0` — orphans `c` and scores
    # `compile error` instead of a verdict. The 225th's rule: prefer a drift
    # that keeps every variable live. Counting one more level instead of
    # recursing keeps `c` referenced AND is a more realistic defect.
    Case(
        "the walk stops one level down, so a deep subtree is undercounted",
        [("\t\tcount += countDescendants(c)", "\t\tcount += len(c.Children)")],
    ),
    Case(
        "the direct children are counted twice",
        [("\tcount := len(s.Children)", "\tcount := len(s.Children) * 2")],
    ),

    # ---- extractIncremental: the request ----
    Case(
        "the request goes to the completions endpoint instead of messages",
        in_request("https://api.anthropic.com/v1/messages",
                   "https://api.anthropic.com/v1/complete"),
    ),
    Case(
        "the API key header is never set, so every call is unauthenticated",
        in_request('\treq.Header.Set("x-api-key", anthropicKey)\n', ""),
    ),
    Case(
        "the anthropic-version header is dropped",
        in_request('\treq.Header.Set("anthropic-version", "2023-06-01")\n', ""),
    ),
    Case(
        "max_tokens is cut to a length that truncates any real reply",
        in_request('"max_tokens": 4096,', '"max_tokens": 64,'),
    ),
    Case(
        "the prompt is sent as an assistant turn instead of a user turn",
        in_request('{"role": "user", "content": prompt},', '{"role": "assistant", "content": prompt},'),
    ),
    Case(
        "the new text is left out of the prompt, so the model analyses nothing",
        [("NEW PORTION to analyze (each line is pre-numbered: [N] (speaker_id) Name: text):\n` + newText + `",
          "NEW PORTION to analyze (each line is pre-numbered: [N] (speaker_id) Name: text):\n` + \"\" + `")],
    ),
    # ⚠️ Replacing `existingSummary` with `""` here orphans the variable and
    # scores `compile error`. Summarising an empty list instead keeps every
    # name live — `existing` is a parameter, and Go permits an unused one — and
    # is the more realistic defect anyway (225th).
    Case(
        "the existing analysis is summarised as empty, so the model re-extracts claims it has",
        [("\texistingSummary := summarizeStatements(existing, 0)",
          "\texistingSummary := summarizeStatements(nil, 0)")],
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
        in_parse('strings.TrimPrefix(text, "```json")', 'strings.TrimPrefix(text, "~~~json")'),
    ),
    Case(
        "the bare ``` fence is no longer stripped",
        in_parse('\ttext = strings.TrimPrefix(text, "```")\n', ""),
    ),
    Case(
        "the trailing fence is left on the end of the reply",
        in_parse('strings.TrimSuffix(text, "```")', 'strings.TrimSuffix(text, "~~~")'),
    ),
    Case(
        "the reply is not trimmed before parsing, so a leading newline breaks it",
        in_parse("\ttext := strings.TrimSpace(result.Content[0].Text)",
                 "\ttext := result.Content[0].Text"),
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
        in_request("\tif resp.StatusCode != 200 {", "\tif resp.StatusCode == -1 {"),
    ),
    Case(
        "the upstream status is dropped from the error, leaving no way to tell 429 from 500",
        in_request('fmt.Errorf("claude API %d: %s", resp.StatusCode, string(body))',
                   'fmt.Errorf("claude API error: %s", string(body))'),
    ),
    Case(
        "the upstream body is dropped from the error, so the reason never reaches the client",
        in_request('fmt.Errorf("claude API %d: %s", resp.StatusCode, string(body))',
                   'fmt.Errorf("claude API %d", resp.StatusCode)'),
    ),
    Case(
        "an empty content array is no longer refused",
        in_parse('\t\treturn nil, fmt.Errorf("empty response")\n\t}\n',
                 '\t\treturn nil, fmt.Errorf("")\n\t}\n'),
    ),

    # ---- the defect characterisations ----
    # These three cases break the CURRENT, defective behaviour. They must be
    # CAUGHT, because the suite pins the defect deliberately: when the bug is
    # fixed the pinning tests fail loudly and name the card. A scorer that let
    # these pass would mean the characterisation is not actually asserted.
    Case(
        "the missing parentheses are added, so an empty statements object parses (defect 7fdc428e is FIXED)",
        [("\tif err := json.Unmarshal([]byte(text), &objResult); err == nil && len(objResult.Statements) > 0 || objResult.Updates != nil {",
          "\tif err := json.Unmarshal([]byte(text), &objResult); err == nil && (len(objResult.Statements) > 0 || objResult.Updates != nil) {")],
    ),
    Case(
        "the object branch is taken whenever the unmarshal succeeded, widening defect 7fdc428e's forward half",
        [("\tif err := json.Unmarshal([]byte(text), &objResult); err == nil && len(objResult.Statements) > 0 || objResult.Updates != nil {",
          "\tif err := json.Unmarshal([]byte(text), &objResult); err == nil {")],
    ),

    # ---- handleAPIAnalyzeIncremental ----
    Case(
        "the method guard admits GET, so a crawler can spend money on this endpoint",
        in_handler_head("\tif r.Method != http.MethodPost {", '\tif r.Method == "NEVER" {'),
    ),
    Case(
        "a non-POST is refused with the wrong status",
        in_handler_head("http.StatusMethodNotAllowed)", "http.StatusBadRequest)"),
    ),
    Case(
        "msg_offset is read from a different JSON key",
        in_handler_head('MsgOffset   int         `json:"msg_offset"`',
                        'MsgOffset   int         `json:"offset"`'),
        expected_unnoticed=(
            "defect 7a0c4490: MsgOffset is forwarded to extractIncremental and then never read, "
            "so no wire key for it can change an observable outcome. This case exists to record "
            "that the dead parameter reaches all the way out to the HTTP contract"
        ),
    ),
    Case(
        "full_review is read from a different JSON key, so the client can never turn review on",
        in_handler_head('FullReview  bool        `json:"full_review"`',
                        'FullReview  bool        `json:"review"`'),
    ),
    Case(
        "context_text is read from a different JSON key",
        in_handler_head('ContextText string      `json:"context_text"`',
                        'ContextText string      `json:"context"`'),
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
        in_handler_head(
            '\t\tif err := json.NewDecoder(r.Body).Decode(&req); err != nil {\n'
            '\t\t\tjsonError(w, "invalid JSON", 400)\n\t\t\treturn\n\t\t}\n',
            "\t\tjson.NewDecoder(r.Body).Decode(&req)\n"),
    ),
    Case(
        "an extraction error becomes a 200 with an empty body instead of a 500",
        in_handler_tail("\tif err != nil {", "\tif err == nil && false {"),
    ),
    Case(
        "the extraction error is swallowed and its text never reaches the client",
        in_handler_tail("jsonError(w, err.Error(), 500)", 'jsonError(w, "", 500)'),
    ),
    Case(
        "an extraction error is reported as a 400, blaming the client for an upstream outage",
        in_handler_tail("jsonError(w, err.Error(), 500)", "jsonError(w, err.Error(), 400)"),
    ),
    Case(
        "full_review is never forwarded, so review mode is unreachable over HTTP",
        in_handler_tail("req.Existing, req.MsgOffset, req.FullReview)",
                        "req.Existing, req.MsgOffset, false)"),
    ),
    Case(
        "context_text is never forwarded, so the model loses the conversation flow",
        in_handler_tail("extractIncremental(req.NewText, req.ContextText,",
                        'extractIncremental(req.NewText, "",'),
    ),
    Case(
        "the existing analysis is never forwarded, so every chunk re-extracts the whole conversation",
        in_handler_tail("req.ContextText, req.Existing, req.MsgOffset",
                        "req.ContextText, nil, req.MsgOffset"),
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
        in_handler_head('strings.Contains(ct, "application/json")', 'strings.Contains(ct, "")'),
    ),
    Case(
        "the response content type is dropped",
        in_handler_tail('\tw.Header().Set("Content-Type", "application/json")\n', ""),
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
        in_request('"model":      "claude-sonnet-4-20250514",',
                   '"model":      "claude-sonnet-4-5-20250929",'),
    ),
    Case(
        "msgOffset is threaded into the numbering it was clearly meant for",
        [("\texistingSummary := summarizeStatements(existing, 0)",
          "\texistingSummary := summarizeStatements(existing, 0)\n\t_ = numberTranscriptLinesOffset(newText, msgOffset)")],
        expected_unnoticed=(
            "defect 7a0c4490: msgOffset is dead, so computing a value and discarding it changes "
            "nothing observable. TestExtractIncrementalIgnoresMsgOffset_DEFECT pins that the "
            "REQUEST is unaffected, which this edit also leaves true. Fixing the defect properly "
            "means feeding the result into the prompt, and that case would be caught"
        ),
    ),
]


if __name__ == "__main__":
    sys.exit(score(TARGETS, PACKAGES, CASES))
