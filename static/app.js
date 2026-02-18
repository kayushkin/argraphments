// Tab switching
function switchTab(name, btn) {
    document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
    document.getElementById('tab-' + name).classList.add('active');
    btn.classList.add('active');
}

// File name display
function updateFileName(input) {
    const label = document.getElementById('file-label');
    label.textContent = input.files[0]?.name || 'Drop audio file here or click to browse';
}

// Recording
let mediaRecorder = null;
let recordedChunks = [];
let recordStart = null;
let recordTimer = null;

function toggleRecording() {
    const btn = document.getElementById('record-btn');
    if (mediaRecorder && mediaRecorder.state === 'recording') {
        stopRecording();
    } else {
        startRecording();
    }
}

async function startRecording() {
    try {
        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        recordedChunks = [];
        mediaRecorder = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' });

        mediaRecorder.ondataavailable = (e) => {
            if (e.data.size > 0) recordedChunks.push(e.data);
        };

        mediaRecorder.onstop = () => {
            stream.getTracks().forEach(t => t.stop());
            const blob = new Blob(recordedChunks, { type: 'audio/webm' });
            uploadRecording(blob);
        };

        mediaRecorder.start();
        recordStart = Date.now();
        updateRecordTime();

        const btn = document.getElementById('record-btn');
        btn.classList.add('recording');
        btn.querySelector('span:last-child') && (btn.lastChild.textContent = ' Stop');
        document.getElementById('record-status').textContent = 'Recording...';
    } catch (err) {
        document.getElementById('record-status').textContent = 'Mic access denied: ' + err.message;
    }
}

function stopRecording() {
    if (mediaRecorder) {
        mediaRecorder.stop();
        clearInterval(recordTimer);
        const btn = document.getElementById('record-btn');
        btn.classList.remove('recording');
        document.getElementById('record-status').textContent = 'Processing...';
    }
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

function uploadRecording(blob) {
    const form = new FormData();
    form.append('audio', blob, 'recording.webm');

    htmx.ajax('POST', '/transcribe', {
        target: '#result',
        swap: 'innerHTML',
        values: form,
    });

    // Manual approach since htmx.ajax with FormData can be tricky
    fetch('/transcribe', { method: 'POST', body: form })
        .then(r => r.text())
        .then(html => {
            document.getElementById('result').innerHTML = html;
            htmx.process(document.getElementById('result'));
        })
        .catch(err => {
            document.getElementById('result').innerHTML = '<div class="error">Upload failed: ' + err.message + '</div>';
        });

    document.getElementById('record-status').textContent = '';
}

// Drag and drop
const dropZone = document.getElementById('drop-zone');
if (dropZone) {
    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.style.borderColor = 'var(--accent)';
    });
    dropZone.addEventListener('dragleave', () => {
        dropZone.style.borderColor = '';
    });
    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        dropZone.style.borderColor = '';
        const input = dropZone.querySelector('input');
        input.files = e.dataTransfer.files;
        updateFileName(input);
    });
}
