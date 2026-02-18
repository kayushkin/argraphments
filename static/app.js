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
let recognition = null;
let finalTranscript = '';
let interimTranscript = '';

function toggleRecording(mode) {
    if (mediaRecorder && mediaRecorder.state === 'recording') {
        stopRecording();
    } else {
        startRecording(mode);
    }
}

async function startRecording(mode) {
    activeMode = mode;
    finalTranscript = '';
    interimTranscript = '';

    // Show live transcript area
    document.getElementById('result').innerHTML = `
        <div class="live-transcript">
            <h2>Live Transcript</h2>
            <div id="live-text" class="live-text"></div>
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

        // Start media recorder for backup Whisper transcription
        recordedChunks = [];
        mediaRecorder = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' });
        mediaRecorder.ondataavailable = (e) => {
            if (e.data.size > 0) recordedChunks.push(e.data);
        };
        mediaRecorder.onstop = () => {
            stream.getTracks().forEach(t => t.stop());
        };
        mediaRecorder.start(1000); // collect in 1s chunks

        // Start speech recognition for live transcription
        startSpeechRecognition();

        recordStart = Date.now();
        updateRecordTime();

        const btn = document.getElementById(mode === 'tab' ? 'tab-record-btn' : 'record-btn');
        btn.classList.add('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = ' Stop';
        document.getElementById('record-status').textContent = mode === 'tab' ? 'Capturing tab audio...' : 'Recording...';
    } catch (err) {
        document.getElementById('record-status').textContent =
            mode === 'tab' ? 'Tab capture failed: ' + err.message : 'Mic access denied: ' + err.message;
    }
}

function startSpeechRecognition() {
    const SpeechRecognition = window.SpeechRecognition || window.webkitSpeechRecognition;
    if (!SpeechRecognition) {
        document.getElementById('live-text').textContent = '(Live transcription not supported in this browser — recording will be sent to Whisper when done)';
        return;
    }

    recognition = new SpeechRecognition();
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.lang = 'en-US';

    recognition.onresult = (event) => {
        interimTranscript = '';
        for (let i = event.resultIndex; i < event.results.length; i++) {
            const text = event.results[i][0].transcript;
            if (event.results[i].isFinal) {
                finalTranscript += text + ' ';
            } else {
                interimTranscript += text;
            }
        }
        updateLiveText();
    };

    recognition.onerror = (event) => {
        // 'no-speech' is normal, just restart
        if (event.error === 'no-speech' || event.error === 'aborted') return;
        console.warn('Speech recognition error:', event.error);
    };

    recognition.onend = () => {
        // Auto-restart if still recording
        if (mediaRecorder && mediaRecorder.state === 'recording') {
            try { recognition.start(); } catch (e) {}
        }
    };

    recognition.start();
}

function updateLiveText() {
    const el = document.getElementById('live-text');
    if (!el) return;
    el.innerHTML = finalTranscript + '<span class="interim">' + interimTranscript + '</span>';
    el.scrollTop = el.scrollHeight;
}

function stopRecording() {
    if (recognition) {
        recognition.onend = null; // prevent restart
        recognition.abort();
        recognition = null;
    }

    if (mediaRecorder) {
        mediaRecorder.stop();
        clearInterval(recordTimer);
        const btn = document.getElementById(activeMode === 'tab' ? 'tab-record-btn' : 'record-btn');
        btn.classList.remove('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = activeMode === 'tab' ? ' Tab Audio' : ' Mic';
        document.getElementById('record-status').textContent = '';
    }

    const transcript = finalTranscript.trim();

    if (transcript) {
        // We have a live transcript — show it in editable form with options
        document.getElementById('result').innerHTML = `
            <div class="transcript-result">
                <h2>Transcript</h2>
                <form hx-post="/argraphments/analyze" hx-target="#result" hx-indicator="#spinner">
                    <textarea name="transcript" rows="12">${escapeHtml(transcript)}</textarea>
                    <div class="transcript-actions">
                        <button type="submit" class="btn">Analyze Structure</button>
                        <button type="button" class="btn btn-secondary" onclick="reprocessWhisper()">Re-transcribe with Whisper</button>
                    </div>
                    <p class="hint">Edit the transcript if needed. Use "Re-transcribe with Whisper" for higher accuracy.</p>
                </form>
            </div>`;
        htmx.process(document.getElementById('result'));
    } else {
        // No live transcript — send to Whisper
        const blob = new Blob(recordedChunks, { type: 'audio/webm' });
        const form = new FormData();
        form.append('audio', blob, 'recording.webm');
        submitAudio(form);
    }
}

function reprocessWhisper() {
    const blob = new Blob(recordedChunks, { type: 'audio/webm' });
    const form = new FormData();
    form.append('audio', blob, 'recording.webm');
    submitAudio(form);
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
