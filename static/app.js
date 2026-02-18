function hideInputs() {
    document.getElementById('header').classList.add('hidden-section');
    document.getElementById('input-section').classList.add('hidden-section');
}

function showInputs() {
    document.getElementById('header').classList.remove('hidden-section');
    document.getElementById('input-section').classList.remove('hidden-section');
    document.getElementById('result').innerHTML = '';
}

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
let pendingTranscribe = false;
let pendingDiarize = false;
let isRecording = false;

// Diarization state
let diarizeData = null;
let speakerNames = {};

const CHUNK_INTERVAL_MS = 10000;

function toggleRecording(mode) {
    if (isRecording) {
        stopRecording();
    } else {
        startRecording(mode);
    }
}

async function startRecording(mode) {
    activeMode = mode;
    fullTranscript = '';
    pendingTranscribe = false;
    pendingDiarize = false;
    isRecording = true;
    diarizeData = null;
    speakerNames = {};

    hideInputs();
    document.getElementById('result').innerHTML = `
        <div class="diarized-view" id="diarized-view">
            <div class="speaker-names" id="speaker-names-section" style="display:none">
                <div class="speaker-list" id="speaker-list"></div>
            </div>
            <div class="chat-messages" id="chat-messages">
                <div class="chat-msg"><span class="chat-text interim">Listening...</span></div>
            </div>
        </div>`;

    try {
        let stream;
        if (mode === 'tab') {
            stream = await navigator.mediaDevices.getDisplayMedia({ video: true, audio: true });
            stream.getVideoTracks().forEach(t => t.stop());
            if (stream.getAudioTracks().length === 0) {
                document.getElementById('record-status').textContent = 'No audio — check "Share tab audio"';
                isRecording = false;
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
        mediaRecorder.onstop = () => stream.getTracks().forEach(t => t.stop());
        mediaRecorder.start(1000);

        recordStart = Date.now();
        updateRecordTime();

        // Pipeline: transcribe chunk → diarize result → update UI, every CHUNK_INTERVAL_MS
        chunkInterval = setInterval(() => processChunk(), CHUNK_INTERVAL_MS);

        const btn = document.getElementById(mode === 'tab' ? 'tab-record-btn' : 'record-btn');
        btn.classList.add('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = ' Stop';
        document.getElementById('record-status').textContent = mode === 'tab' ? 'Capturing tab audio...' : 'Recording...';
    } catch (err) {
        isRecording = false;
        document.getElementById('record-status').textContent = err.message;
    }
}

async function processChunk() {
    if (pendingTranscribe || recordedChunks.length === 0) return;
    pendingTranscribe = true;

    const blob = new Blob(recordedChunks, { type: 'audio/webm' });
    const form = new FormData();
    form.append('audio', blob, 'chunk.webm');

    try {
        const resp = await fetch('/argraphments/transcribe-raw', { method: 'POST', body: form });
        if (resp.ok) {
            const text = (await resp.text()).trim();
            if (text && text !== fullTranscript) {
                fullTranscript = text;
                // Diarize in background (don't block next transcribe)
                if (!pendingDiarize) {
                    pendingDiarize = true;
                    diarizeAsync(fullTranscript).finally(() => { pendingDiarize = false; });
                }
            }
        }
    } catch (e) {
        console.warn('Chunk failed:', e);
    } finally {
        pendingTranscribe = false;
    }
}

async function diarizeAsync(transcript) {
    try {
        const form = new FormData();
        form.append('transcript', transcript);
        const resp = await fetch('/argraphments/diarize', { method: 'POST', body: form });
        const data = await resp.json();
        if (data.error) return;

        diarizeData = data;
        // Merge detected names with any user edits
        for (const [id, name] of Object.entries(data.speakers)) {
            if (!speakerNames[id]) {
                speakerNames[id] = name || id.replace('_', ' ').replace(/\b\w/g, c => c.toUpperCase());
            } else if (name && speakerNames[id] === id.replace('_', ' ').replace(/\b\w/g, c => c.toUpperCase())) {
                // Claude detected a real name, update if user hasn't manually changed it
                speakerNames[id] = name;
            }
        }
        renderChatMessages();
    } catch (e) {
        console.warn('Diarize failed:', e);
    }
}

const speakerColors = ['#7c6ff0', '#6ec1e4', '#e4c76e', '#7ce4a1', '#e47070', '#b070e4'];

function getColorMap() {
    const ids = Object.keys(speakerNames);
    const map = {};
    ids.forEach((id, i) => { map[id] = speakerColors[i % speakerColors.length]; });
    return map;
}

function renderChatMessages() {
    if (!diarizeData) return;
    const colorMap = getColorMap();

    const section = document.getElementById('speaker-names-section');
    const list = document.getElementById('speaker-list');
    if (section && list) {
        section.style.display = '';
        let html = '';
        for (const id of Object.keys(speakerNames)) {
            const color = colorMap[id] || '#888';
            html += `
                <div class="speaker-input" style="--speaker-color: ${color}">
                    <span class="speaker-dot" style="background: ${color}"></span>
                    <input type="text" value="${escapeHtml(speakerNames[id])}"
                           onchange="renameSpeaker('${id}', this.value)"
                           data-speaker="${id}">
                </div>`;
        }
        list.innerHTML = html;
    }

    // Update messages
    const container = document.getElementById('chat-messages');
    if (!container) return;

    let html = '';
    for (const msg of diarizeData.messages) {
        const name = speakerNames[msg.speaker] || msg.speaker;
        const color = colorMap[msg.speaker] || '#888';
        html += `
            <div class="chat-msg" data-speaker="${msg.speaker}">
                <span class="chat-speaker" style="color: ${color}">${escapeHtml(name)}</span>
                <span class="chat-text">${escapeHtml(msg.text)}</span>
            </div>`;
    }
    container.innerHTML = html;
    container.scrollTop = container.scrollHeight;
}

function renameSpeaker(id, newName) {
    speakerNames[id] = newName;
    document.querySelectorAll(`.chat-msg[data-speaker="${id}"] .chat-speaker`).forEach(el => {
        el.textContent = newName;
    });
}

function buildTranscriptText() {
    if (!diarizeData) return fullTranscript;
    return diarizeData.messages
        .map(m => `${speakerNames[m.speaker]}: ${m.text}`)
        .join('\n');
}

function stopRecording() {
    isRecording = false;
    clearInterval(chunkInterval);

    if (mediaRecorder) {
        mediaRecorder.stop();
        clearInterval(recordTimer);
        const btn = document.getElementById(activeMode === 'tab' ? 'tab-record-btn' : 'record-btn');
        btn.classList.remove('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = activeMode === 'tab' ? ' Tab Audio' : ' Mic';
        document.getElementById('record-status').textContent = 'Finalizing...';
    }

    if (recordedChunks.length > 0) {
        // Final transcribe + diarize pass
        const blob = new Blob(recordedChunks, { type: 'audio/webm' });
        const form = new FormData();
        form.append('audio', blob, 'recording.webm');

        fetch('/argraphments/transcribe-raw', { method: 'POST', body: form })
            .then(r => r.text())
            .then(async text => {
                fullTranscript = text.trim() || fullTranscript;
                await diarizeAsync(fullTranscript);
                showFinalView();
            })
            .catch(() => showFinalView());
    } else {
        showFinalView();
    }
}

function showFinalView() {
    document.getElementById('record-status').textContent = '';

    // Add the analyze button below the chat
    const container = document.getElementById('diarized-view');
    if (!container) return;

    // Remove any existing analyze form
    const existing = container.querySelector('.analyze-form');
    if (existing) existing.remove();

    const formDiv = document.createElement('div');
    formDiv.className = 'analyze-form';
    formDiv.innerHTML = `
        <div class="action-row">
            <form hx-post="/argraphments/analyze" hx-target="#result" hx-indicator="#spinner" style="display:inline">
                <textarea name="transcript" style="display:none"></textarea>
                <button type="submit" class="btn" onclick="this.previousElementSibling.value = buildTranscriptText()">Analyze Structure</button>
            </form>
            <button class="btn btn-secondary" onclick="showInputs()">New</button>
        </div>`;
    container.appendChild(formDiv);
    htmx.process(formDiv);
}

// --- Audio upload → transcribe → diarize ---

async function submitAudioForDiarize(form) {
    hideInputs();
    document.getElementById('spinner').classList.add('htmx-request');
    document.getElementById('result').innerHTML = `
        <div class="diarized-view" id="diarized-view">
            <div class="speaker-names" id="speaker-names-section" style="display:none">
                <div class="speaker-list" id="speaker-list"></div>
            </div>
            <div class="chat-messages" id="chat-messages">
                <div class="chat-msg"><span class="chat-text interim">Transcribing...</span></div>
            </div>
        </div>`;

    try {
        const resp = await fetch('/argraphments/transcribe-raw', { method: 'POST', body: form });
        const text = (await resp.text()).trim();
        document.getElementById('spinner').classList.remove('htmx-request');

        if (!text) {
            document.getElementById('result').innerHTML = '<div class="error">No speech detected</div>';
            return;
        }

        fullTranscript = text;
        diarizeData = null;
        speakerNames = {};

        const chatEl = document.getElementById('chat-messages');
        if (chatEl) chatEl.innerHTML = '<div class="chat-msg"><span class="chat-text interim">Identifying speakers...</span></div>';

        await diarizeAsync(text);
        showFinalView();
    } catch (err) {
        document.getElementById('spinner').classList.remove('htmx-request');
        document.getElementById('result').innerHTML = '<div class="error">Failed: ' + err.message + '</div>';
    }
}

// --- Paste text → diarize ---

async function submitPasteForDiarize(form) {
    const text = form.querySelector('textarea').value.trim();
    if (!text) return;

    hideInputs();
    fullTranscript = text;
    diarizeData = null;
    speakerNames = {};

    document.getElementById('result').innerHTML = `
        <div class="diarized-view" id="diarized-view">
            <div class="speaker-names" id="speaker-names-section" style="display:none">
                <div class="speaker-list" id="speaker-list"></div>
            </div>
            <div class="chat-messages" id="chat-messages">
                <div class="chat-msg"><span class="chat-text interim">Identifying speakers...</span></div>
            </div>
        </div>`;

    await diarizeAsync(text);
    showFinalView();
}

// --- Utilities ---

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
