// Upload file
function uploadFile(input) {
    if (!input.files[0]) return;
    const form = new FormData();
    form.append('audio', input.files[0]);
    submitAudioForDiarize(form);
    input.value = '';
}

// Recording state
let mediaRecorder = null;
let recordedChunks = [];
let recordStart = null;
let recordTimer = null;
let activeMode = null;
let chunkInterval = null;
let fullTranscript = '';
let pendingChunk = false;

// Diarization state
let diarizeData = null;
let speakerNames = {};

const CHUNK_INTERVAL_MS = 10000;

function toggleRecording(mode) {
    if (mediaRecorder && mediaRecorder.state === 'recording') {
        stopRecording();
    } else {
        startRecording(mode);
    }
}

async function startRecording(mode) {
    activeMode = mode;
    fullTranscript = '';
    pendingChunk = false;

    document.getElementById('result').innerHTML = `
        <div class="live-transcript">
            <h2>Live Transcript</h2>
            <div id="live-text" class="live-text"><span class="interim">Listening...</span></div>
        </div>`;

    try {
        let stream;
        if (mode === 'tab') {
            stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
            stream.getVideoTracks().forEach(t => t.stop());
            if (stream.getAudioTracks().length === 0) {
                document.getElementById('record-status').textContent = 'No audio track — make sure you select "Share tab audio"';
                return;
            }
        } else {
            stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        }

        recordedChunks = [];
        mediaRecorder = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' });
        mediaRecorder.ondataavailable = (e) => {
            if (e.data.size > 0) recordedChunks.push(e.data);
        };
        mediaRecorder.onstop = () => {
            stream.getTracks().forEach(t => t.stop());
        };
        mediaRecorder.start(1000);

        recordStart = Date.now();
        updateRecordTime();
        chunkInterval = setInterval(() => sendChunk(), CHUNK_INTERVAL_MS);

        const btn = document.getElementById(mode === 'tab' ? 'tab-record-btn' : 'record-btn');
        btn.classList.add('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = ' Stop';
        document.getElementById('record-status').textContent = mode === 'tab' ? 'Capturing tab audio...' : 'Recording...';
    } catch (err) {
        document.getElementById('record-status').textContent =
            mode === 'tab' ? 'Tab capture failed: ' + err.message : 'Mic access denied: ' + err.message;
    }
}

async function sendChunk() {
    if (pendingChunk || recordedChunks.length === 0) return;
    pendingChunk = true;
    const blob = new Blob(recordedChunks, { type: 'audio/webm' });
    const form = new FormData();
    form.append('audio', blob, 'chunk.webm');
    try {
        const resp = await fetch('/argraphments/transcribe-raw', { method: 'POST', body: form });
        if (resp.ok) {
            const text = await resp.text();
            if (text.trim()) {
                fullTranscript = text.trim();
                const el = document.getElementById('live-text');
                if (el) {
                    el.textContent = fullTranscript;
                    el.scrollTop = el.scrollHeight;
                }
            }
        }
    } catch (e) {
        console.warn('Chunk transcription failed:', e);
    } finally {
        pendingChunk = false;
    }
}

function stopRecording() {
    clearInterval(chunkInterval);
    if (mediaRecorder) {
        mediaRecorder.stop();
        clearInterval(recordTimer);
        const btn = document.getElementById(activeMode === 'tab' ? 'tab-record-btn' : 'record-btn');
        btn.classList.remove('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = activeMode === 'tab' ? ' Tab Audio' : ' Mic';
        document.getElementById('record-status').textContent = '';
    }

    if (recordedChunks.length > 0) {
        document.getElementById('result').innerHTML = `
            <div class="live-transcript">
                <h2>Transcript</h2>
                <div id="live-text" class="live-text">${escapeHtml(fullTranscript)}<span class="interim"> finalizing...</span></div>
            </div>`;

        // Final Whisper pass then diarize
        const blob = new Blob(recordedChunks, { type: 'audio/webm' });
        const form = new FormData();
        form.append('audio', blob, 'recording.webm');

        fetch('/argraphments/transcribe-raw', { method: 'POST', body: form })
            .then(r => r.text())
            .then(text => {
                const final = text.trim() || fullTranscript;
                diarize(final);
            })
            .catch(() => diarize(fullTranscript));
    } else if (fullTranscript) {
        diarize(fullTranscript);
    }
}

// --- Diarization ---

async function diarize(transcript) {
    document.getElementById('result').innerHTML = `
        <div class="live-transcript">
            <h2>Identifying speakers...</h2>
            <div id="live-text" class="live-text">${escapeHtml(transcript)}</div>
        </div>`;

    try {
        const form = new FormData();
        form.append('transcript', transcript);
        const resp = await fetch('/argraphments/diarize', { method: 'POST', body: form });
        const data = await resp.json();

        if (data.error) {
            showEditableTranscript(transcript);
            return;
        }

        diarizeData = data;
        speakerNames = {};
        for (const [id, name] of Object.entries(data.speakers)) {
            speakerNames[id] = name || id.replace('_', ' ').replace(/\b\w/g, c => c.toUpperCase());
        }
        renderChatView();
    } catch (e) {
        console.warn('Diarization failed:', e);
        showEditableTranscript(transcript);
    }
}

function renderChatView() {
    const speakerColors = ['#7c6ff0', '#6ec1e4', '#e4c76e', '#7ce4a1', '#e47070', '#b070e4'];
    const speakerIds = Object.keys(speakerNames);
    const colorMap = {};
    speakerIds.forEach((id, i) => { colorMap[id] = speakerColors[i % speakerColors.length]; });

    let speakerInputs = '<div class="speaker-names"><h3>Speakers</h3><div class="speaker-list">';
    for (const id of speakerIds) {
        const color = colorMap[id];
        speakerInputs += `
            <div class="speaker-input" style="--speaker-color: ${color}">
                <span class="speaker-dot" style="background: ${color}"></span>
                <input type="text" value="${escapeHtml(speakerNames[id])}" 
                       onchange="renameSpeaker('${id}', this.value)" 
                       data-speaker="${id}">
            </div>`;
    }
    speakerInputs += '</div></div>';

    let messages = '<div class="chat-messages">';
    for (const msg of diarizeData.messages) {
        const name = speakerNames[msg.speaker] || msg.speaker;
        const color = colorMap[msg.speaker] || '#888';
        messages += `
            <div class="chat-msg" data-speaker="${msg.speaker}">
                <span class="chat-speaker" style="color: ${color}">${escapeHtml(name)}</span>
                <span class="chat-text">${escapeHtml(msg.text)}</span>
            </div>`;
    }
    messages += '</div>';

    // Build transcript text for analyze
    const transcriptText = diarizeData.messages
        .map(m => `${speakerNames[m.speaker]}: ${m.text}`)
        .join('\n');

    document.getElementById('result').innerHTML = `
        <div class="diarized-view">
            ${speakerInputs}
            ${messages}
            <form hx-post="/argraphments/analyze" hx-target="#result" hx-indicator="#spinner">
                <textarea name="transcript" rows="1" style="display:none">${escapeHtml(transcriptText)}</textarea>
                <button type="submit" class="btn" onclick="this.previousElementSibling.value = buildTranscriptText()">Analyze Structure</button>
            </form>
        </div>`;
    htmx.process(document.getElementById('result'));
}

function renameSpeaker(id, newName) {
    speakerNames[id] = newName;
    // Update all message labels with this speaker
    document.querySelectorAll(`.chat-msg[data-speaker="${id}"] .chat-speaker`).forEach(el => {
        el.textContent = newName;
    });
}

function buildTranscriptText() {
    return diarizeData.messages
        .map(m => `${speakerNames[m.speaker]}: ${m.text}`)
        .join('\n');
}

// --- Audio upload with diarization ---

function submitAudioForDiarize(form) {
    document.getElementById('spinner').classList.add('htmx-request');
    fetch('/argraphments/transcribe-raw', { method: 'POST', body: form })
        .then(r => r.text())
        .then(text => {
            document.getElementById('spinner').classList.remove('htmx-request');
            if (text.trim()) {
                diarize(text.trim());
            } else {
                document.getElementById('result').innerHTML = '<div class="error">No speech detected in audio</div>';
            }
        })
        .catch(err => {
            document.getElementById('spinner').classList.remove('htmx-request');
            document.getElementById('result').innerHTML = '<div class="error">Upload failed: ' + err.message + '</div>';
        });
}

// --- Paste text analyze (goes through diarize first) ---

function submitPasteForDiarize(form) {
    const textarea = form.querySelector('textarea');
    const text = textarea.value.trim();
    if (!text) return;
    diarize(text);
}

// --- Fallback ---

function showEditableTranscript(transcript) {
    document.getElementById('result').innerHTML = `
        <div class="transcript-result">
            <h2>Transcript</h2>
            <form hx-post="/argraphments/analyze" hx-target="#result" hx-indicator="#spinner">
                <textarea name="transcript" rows="12">${escapeHtml(transcript)}</textarea>
                <p class="hint">Edit the transcript if needed, then analyze.</p>
                <button type="submit" class="btn">Analyze Structure</button>
            </form>
        </div>`;
    htmx.process(document.getElementById('result'));
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function updateRecordTime() {
    const el = document.getElementById('record-time');
    recordTimer = setInterval(() => {
        const elapsed = Math.floor((Date.now() - recordStart) / 1000);
        const m = String(Math.floor(elapsed / 60)).padStart(2, '0');
        const s = String(elapsed % 60).padStart(2, '0');
        el.textContent = m + ':' + s;
    }, 1000);
}

// Legacy for transcribe HTML endpoint
function submitAudio(form) {
    document.getElementById('spinner').classList.add('htmx-request');
    fetch('/argraphments/transcribe', { method: 'POST', body: form })
        .then(r => r.text())
        .then(html => {
            document.getElementById('result').innerHTML = html;
            htmx.process(document.getElementById('result'));
        })
        .catch(err => {
            document.getElementById('result').innerHTML = '<div class="error">Upload failed: ' + err.message + '</div>';
        })
        .finally(() => {
            document.getElementById('spinner').classList.remove('htmx-request');
        });
}
