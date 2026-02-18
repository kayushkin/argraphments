// Upload file
function uploadFile(input) {
    if (!input.files[0]) return;
    const form = new FormData();
    form.append('audio', input.files[0]);
    submitAudio(form);
    input.value = '';
}

// Recording
let mediaRecorder = null;
let recordedChunks = [];
let recordStart = null;
let recordTimer = null;
let activeMode = null;
let chunkInterval = null;
let fullTranscript = '';
let chunkIndex = 0;
let pendingChunk = false;

const CHUNK_INTERVAL_MS = 10000; // send to Whisper every 10s

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
    chunkIndex = 0;
    pendingChunk = false;

    document.getElementById('result').innerHTML = `
        <div class="live-transcript">
            <h2>Live Transcript</h2>
            <div id="live-text" class="live-text"><span class="interim">Listening...</span></div>
        </div>`;

    try {
        let stream;
        if (mode === 'tab') {
            stream = await navigator.mediaDevices.getDisplayMedia({
                video: true,
                audio: true,
            });
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

        // Collect data frequently so we can slice chunks
        mediaRecorder.start(1000);

        recordStart = Date.now();
        updateRecordTime();

        // Send chunks to Whisper periodically
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

    // Send all accumulated audio so far (full recording up to now)
    // This way Whisper has full context and we just take the new text
    const blob = new Blob(recordedChunks, { type: 'audio/webm' });
    const form = new FormData();
    form.append('audio', blob, 'chunk.webm');

    try {
        const resp = await fetch('/argraphments/transcribe-raw', { method: 'POST', body: form });
        if (resp.ok) {
            const text = await resp.text();
            if (text.trim()) {
                fullTranscript = text.trim();
                updateLiveText();
            }
        }
    } catch (e) {
        console.warn('Chunk transcription failed:', e);
    } finally {
        pendingChunk = false;
    }
}

function updateLiveText() {
    const el = document.getElementById('live-text');
    if (!el) return;
    el.textContent = fullTranscript;
    el.scrollTop = el.scrollHeight;
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

    // Do one final transcription of the complete recording
    if (recordedChunks.length > 0) {
        const blob = new Blob(recordedChunks, { type: 'audio/webm' });
        const form = new FormData();
        form.append('audio', blob, 'recording.webm');

        document.getElementById('result').innerHTML = `
            <div class="live-transcript">
                <h2>Transcript</h2>
                <div id="live-text" class="live-text">${escapeHtml(fullTranscript)}<span class="interim"> finalizing...</span></div>
            </div>`;

        fetch('/argraphments/transcribe-raw', { method: 'POST', body: form })
            .then(r => r.text())
            .then(text => {
                const final = text.trim() || fullTranscript;
                showEditableTranscript(final);
            })
            .catch(() => showEditableTranscript(fullTranscript));
    } else if (fullTranscript) {
        showEditableTranscript(fullTranscript);
    }
}

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

function submitAudio(form) {
    document.getElementById('spinner').classList.add('htmx-request');
    fetch('/argraphments/transcribe', { method: 'POST', body: form })
        .then(r => r.text())
        .then(html => {
            document.getElementById('result').innerHTML = html;
            htmx.process(document.getElementById('result'));
            document.getElementById('record-status').textContent = '';
        })
        .catch(err => {
            document.getElementById('result').innerHTML = '<div class="error">Upload failed: ' + err.message + '</div>';
        })
        .finally(() => {
            document.getElementById('spinner').classList.remove('htmx-request');
        });
}
