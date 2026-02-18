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

function toggleRecording() {
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
            const form = new FormData();
            form.append('audio', blob, 'recording.webm');
            submitAudio(form);
        };

        mediaRecorder.start();
        recordStart = Date.now();
        updateRecordTime();

        const btn = document.getElementById('record-btn');
        btn.classList.add('recording');
        btn.childNodes[btn.childNodes.length - 1].textContent = ' Stop';
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
        btn.childNodes[btn.childNodes.length - 1].textContent = ' Record';
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
