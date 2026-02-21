const BASE_PATH = window.location.pathname.startsWith('/argraphments') ? '/argraphments' : '';
let currentSlug = null;

async function createSession() {
    try {
        const resp = await fetch(BASE_PATH + '/api/session/new', { method: 'POST' });
        const data = await resp.json();
        currentSlug = data.slug;
        history.pushState({ slug: currentSlug }, '', BASE_PATH + '/' + currentSlug);
        return currentSlug;
    } catch (e) {
        console.warn('Failed to create session:', e);
        return null;
    }
}

function hideInputs() {
    document.getElementById('input-section').classList.add('hidden-section');
}

function showInputs() {
    document.getElementById('input-section').classList.remove('hidden-section');
    document.getElementById('result').innerHTML = '';
    currentSlug = null;
    speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();
    diarizeData = null;
    fullTranscript = '';
    analyzedStatements = [];
    lastAnalyzedTranscript = '';
    hideSessionHeader();
    document.title = 'argraphments';
    currentSourceURL = '';
    currentSourceTitle = '';
    currentSegments = [];
    const ytEmbed = document.getElementById('yt-embed-container');
    if (ytEmbed) ytEmbed.remove();
    // Clear the textarea
    const ta = document.querySelector('form textarea[name="transcript"]');
    if (ta) ta.value = '';
    history.pushState(null, '', BASE_PATH + '/');
    loadDiscovery();
}

// Sample conversations generated server-side via /api/sample

let currentSourceURL = '';
let currentSourceTitle = '';
let currentSegments = [];

// YouTube import
async function importYouTube(url) {
    if (!url || !url.trim()) return;
    url = url.trim();
    const videoId = extractYouTubeId(url);
    if (!videoId) {
        const status = document.getElementById('yt-status');
        if (status) { status.textContent = 'Invalid YouTube URL'; status.className = 'yt-status error'; }
        return;
    }

    currentSourceURL = url;
    currentSegments = [];

    // Start a recording session
    isRecording = true;
    diarizeData = null;
    speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();
    await createSession();
    hideInputs();
    lastAnalyzedTranscript = '';
    analyzedStatements = [];
    statementIdCounter = 0;
    initLiveSession('Starting YouTube playback...');

    // Embed the video
    const resultEl = document.getElementById('result');
    const embedContainer = document.createElement('div');
    embedContainer.id = 'yt-live-embed';
    embedContainer.innerHTML = `
        <div class="yt-embed-wrapper" style="margin-bottom: 1rem;">
            <iframe id="yt-live-iframe" src="https://www.youtube.com/embed/${videoId}?autoplay=1&enablejsapi=1"
                frameborder="0" allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; autoplay"
                allowfullscreen></iframe>
        </div>
`;
    resultEl.insertBefore(embedContainer, resultEl.firstChild);

    // Capture tab audio — preferCurrentTab auto-selects this tab in Chrome
    try {
        const stream = await navigator.mediaDevices.getDisplayMedia({
            video: true,
            audio: true,
            preferCurrentTab: true,
            selfBrowserSurface: 'include',
        });
        stream.getVideoTracks().forEach(t => t.stop());
        if (stream.getAudioTracks().length === 0) {
            document.getElementById('record-status').textContent = 'No audio — check "Share tab audio"';
            isRecording = false;
            return;
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
        chunkInterval = setInterval(() => processChunk(), CHUNK_INTERVAL_MS);

        showSessionHeader(true);
        document.getElementById('record-status').textContent = 'Recording YouTube audio...';

        // Fetch title in the background
        fetch(BASE_PATH + '/api/import/youtube', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url, title_only: true })
        }).then(r => r.json()).then(data => {
            if (data.title) {
                currentSourceTitle = data.title;
                updateConversationTitle(data.title, url);
            }
        }).catch(() => {});

    } catch (err) {
        isRecording = false;
        const status = document.getElementById('record-status');
        if (status) status.textContent = err.message;
        const embed = document.getElementById('yt-live-embed');
        if (embed) embed.remove();
    }
}

function updateConversationTitle(title, sourceURL) {
    if (!title) return;
    document.title = title + ' — argraphments';
    const bar = document.getElementById('recording-bar');
    if (!bar) return;
    let titleEl = bar.querySelector('.conversation-title-bar');
    if (!titleEl) {
        titleEl = document.createElement('div');
        titleEl.className = 'conversation-title-bar';
        bar.querySelector('.recording-bar-top').appendChild(titleEl);
    }
    const url = sourceURL || currentSourceURL;
    if (url && isYouTubeURL(url)) {
        const videoId = extractYouTubeId(url);
        titleEl.innerHTML = `
            <span class="yt-title-group">
                <svg class="yt-icon" viewBox="0 0 28 20" xmlns="http://www.w3.org/2000/svg"><path d="M27.4 3.1s-.3-1.8-1.1-2.6C25.2-.1 23.9-.1 23.3-.2 19.4-.4 14-.4 14-.4s-5.4 0-9.3.2c-.6.1-1.9.1-3 1.5C.9 2.3.6 4.1.6 4.1S.3 6.2.3 8.4v2c0 2.2.3 4.3.3 4.3s.3 1.8 1.1 2.6c1.1 1.1 2.4 1.1 3 1.2 2.2.2 9.3.3 9.3.3s5.4 0 9.3-.2c.6-.1 1.9-.1 3-1.5.8-.8 1.1-2.6 1.1-2.6s.3-2.2.3-4.3v-2c0-2.2-.3-4.3-.3-4.3z" fill="#FF0000"/><path d="M11.2 13.2V5.6l7.8 3.8-7.8 3.8z" fill="#FFF"/></svg>
                <a href="javascript:void(0)" class="source-link yt-dropdown-link" onclick="toggleYouTubeEmbed('${videoId}')" title="Click to show/hide video">${escapeHtml(title)} <span class="yt-chevron">▾</span></a>
            </span>`;
    } else if (url) {
        titleEl.innerHTML = `<a href="${escapeHtml(url)}" target="_blank" rel="noopener" class="source-link">${escapeHtml(title)}</a>`;
    } else {
        titleEl.textContent = title;
    }
}

function isYouTubeURL(url) {
    return /youtube\.com\/watch|youtu\.be\//.test(url);
}

function extractYouTubeId(url) {
    const m = url.match(/(?:v=|youtu\.be\/)([a-zA-Z0-9_-]{11})/);
    return m ? m[1] : '';
}

function toggleYouTubeEmbed(videoId) {
    const chevron = document.querySelector('.yt-chevron');
    const existing = document.getElementById('yt-embed-container');
    if (existing) {
        existing.remove();
        if (chevron) chevron.textContent = '▾';
        return;
    }
    if (chevron) chevron.textContent = '▴';
    const container = document.createElement('div');
    container.id = 'yt-embed-container';
    container.innerHTML = `
        <div class="yt-embed-wrapper">
            <iframe src="https://www.youtube.com/embed/${videoId}" frameborder="0"
                allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
                allowfullscreen></iframe>
        </div>`;
    // Insert after the recording bar
    const bar = document.getElementById('recording-bar');
    if (bar && bar.parentNode) {
        bar.parentNode.insertBefore(container, bar.nextSibling);
    } else {
        document.getElementById('result').prepend(container);
    }
}

function toggleSection(id) {
    const section = document.getElementById(id);
    if (!section) return;
    const body = section.querySelector('.section-body');
    const btn = section.querySelector('.btn-minimize');
    if (!body) return;
    const collapsed = body.style.display === 'none';
    body.style.display = collapsed ? '' : 'none';
    if (btn) btn.textContent = collapsed ? '−' : '+';
    section.classList.toggle('collapsed', !collapsed);
}

async function submitTestConvo() {
    const status = document.getElementById('yt-status');
    if (status) {
        status.textContent = 'Generating sample conversation...';
        status.className = 'yt-status loading';
    }

    try {
        const resp = await fetch(BASE_PATH + '/api/sample', { method: 'POST' });
        const data = await resp.json();
        if (!resp.ok) {
            if (status) { status.textContent = data.error || 'Failed'; status.className = 'yt-status error'; }
            return;
        }

        if (status) status.textContent = '';
        currentSourceURL = data.url || '';
        currentSegments = [];

        // Set up diarize data directly from the response
        diarizeData = { speakers: data.speakers, messages: data.messages };
        assignWordBasedTimestamps(diarizeData.messages);
        fullTranscript = data.text;
        speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();
        for (const [id, name] of Object.entries(data.speakers)) {
            speakerNames[id] = name || pickAnonName();
            if (!name) speakerAutoGen[id] = true;
        }

        hideInputs();
        await createSession();
        showSessionHeader(false);
        initLiveSession('');
        renderChatMessages();
        if (data.title) updateConversationTitle(data.title, data.url);

        // Run analysis
        const transcript = buildTranscriptText();
        await analyzeAsync(transcript);
    } catch (e) {
        if (status) { status.textContent = 'Failed: ' + e.message; status.className = 'yt-status error'; }
    }
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
let pendingAnalyze = false;
let isRecording = false;

// Diarization state
let diarizeData = null;
let speakerNames = {};
let speakerAutoGen = {};  // speaker local_id → bool
let speakerDbIds = {};    // speaker local_id → DB id

const anonNames = [
    'Alex', 'Blake', 'Casey', 'Dana', 'Eden', 'Finn', 'Gray', 'Harper',
    'Ivy', 'Jay', 'Kit', 'Lane', 'Morgan', 'Noel', 'Oak', 'Parker',
    'Quinn', 'Ray', 'Sam', 'Tate', 'Val', 'Wren', 'Zara', 'Sage',
    'Ash', 'Brook', 'Drew', 'Ellis', 'Fern', 'Glen', 'Haven', 'Jade',
    'Kai', 'Lark', 'Maple', 'Nico', 'Olive', 'Pax', 'Reed', 'Sky',
];
const usedAnonNames = new Set();
function pickAnonName() {
    const available = anonNames.filter(n => !usedAnonNames.has(n));
    const name = available.length > 0
        ? available[Math.floor(Math.random() * available.length)]
        : anonNames[Math.floor(Math.random() * anonNames.length)] + Math.floor(Math.random() * 99);
    usedAnonNames.add(name);
    return name;
}
function isDefaultSpeakerName(name) {
    return /^speaker[_ ]\d+$/i.test(name);
}
let lastAnalyzedTranscript = '';
let analyzedStatements = []; // accumulated argument structure

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
    speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();
    await createSession();

    hideInputs();
    lastAnalyzedTranscript = '';
    analyzedStatements = [];
    statementIdCounter = 0;
    initLiveSession('Listening...');

    try {
        let stream;
        if (mode === 'tab') {
            stream = await navigator.mediaDevices.getDisplayMedia({
                video: true, audio: true,
                preferCurrentTab: true, selfBrowserSurface: 'include',
            });
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

        chunkInterval = setInterval(() => processChunk(), CHUNK_INTERVAL_MS);

        showSessionHeader(true);
        document.getElementById('record-status').textContent = '';
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
        const resp = await fetch(BASE_PATH + '/api/transcribe', { method: 'POST', body: form });
        if (resp.ok) {
            const data = await resp.json();
            const text = (data.text || '').trim();
            if (text && text !== fullTranscript) {
                fullTranscript = text;
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
        const resp = await fetch(BASE_PATH + '/api/diarize', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ transcript, segments: currentSegments.length ? currentSegments : undefined })
        });
        const data = await resp.json();
        if (data.error) return;

        diarizeData = data;
        assignWordBasedTimestamps(diarizeData.messages);
        for (const [id, name] of Object.entries(data.speakers)) {
            if (!speakerNames[id]) {
                if (!name || isDefaultSpeakerName(name)) {
                    speakerNames[id] = pickAnonName();
                    speakerAutoGen[id] = true;
                } else {
                    speakerNames[id] = name;
                }
            } else if (name && !isDefaultSpeakerName(name) && speakerAutoGen[id]) {
                // Server gave a real name, upgrade from anon
                speakerNames[id] = name;
                speakerAutoGen[id] = false;
            }
        }
        renderChatMessages();

        const currentTranscript = buildTranscriptText();
        if (!pendingAnalyze && currentTranscript.length > lastAnalyzedTranscript.length + 50) {
            pendingAnalyze = true;
            analyzeAsync(currentTranscript).finally(() => { pendingAnalyze = false; });
        }
    } catch (e) {
        console.warn('Diarize failed:', e);
    }
}

async function analyzeAsync(transcript, forceFullReanalysis) {
    try {
        let resp;

        if (!forceFullReanalysis && lastAnalyzedTranscript && transcript.startsWith(lastAnalyzedTranscript.substring(0, 50))) {
            const newText = transcript.substring(lastAnalyzedTranscript.length).trim();
            if (!newText || newText.length < 20) return;
            // Count messages already analyzed for msg_offset
            const prevLines = lastAnalyzedTranscript.split('\n').filter(l => l.trim()).length;
            resp = await fetch(BASE_PATH + '/api/analyze-incremental', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ new_text: newText, existing: analyzedStatements, msg_offset: prevLines })
            });
        } else {
            // Full analysis — include diarization data for persistence
            const body = { transcript };
            if (currentSlug) body.slug = currentSlug;
            if (currentSourceURL) body.source_url = currentSourceURL;
            if (diarizeData) {
                body.speakers = Object.keys(speakerNames).length > 0 ? speakerNames : diarizeData.speakers;
                body.messages = diarizeData.messages;
                body.speaker_auto_gen = speakerAutoGen;
            }
            resp = await fetch(BASE_PATH + '/api/analyze', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body)
            });
        }

        if (!resp.ok) return;
        const data = await resp.json();

        lastAnalyzedTranscript = transcript;

        if (data.transcript_id) {
            // Full analysis response
            analyzedStatements = data.statements || [];
            if (data.title) updateConversationTitle(data.title, currentSourceURL);
        } else {
            // Incremental response
            const newStatements = data.statements || [];
            for (const s of newStatements) {
                if (s.parent_text) {
                    const parent = findStatementByText(analyzedStatements, s.parent_text);
                    if (parent) {
                        if (!parent.children) parent.children = [];
                        parent.children.push(s);
                        continue;
                    }
                }
                analyzedStatements.push(s);
            }
        }

        renderArgumentTree();
    } catch (e) {
        console.warn('Analyze failed:', e);
    }
}

function findStatementByText(statements, text) {
    const needle = text.toLowerCase().trim();
    for (const s of statements) {
        if (s.text && s.text.toLowerCase().trim() === needle) return s;
        if (s.children) {
            const found = findStatementByText(s.children, text);
            if (found) return found;
        }
    }
    return null;
}

// --- Live session template ---

function initLiveSession(statusText) {
    const tmpl = document.getElementById('tmpl-live-session');
    const clone = tmpl.content.cloneNode(true);
    clone.getElementById('session-status').textContent = statusText || '';
    const result = document.getElementById('result');
    result.innerHTML = '';
    result.appendChild(clone);
}

// --- Session header (recording bar) controls ---

function showSessionHeader(recording) {
    const bar = document.getElementById('recording-bar');
    if (!bar) return;
    bar.style.display = '';
    bar.querySelectorAll('.record-dot, .record-time, .btn-stop').forEach(el => {
        el.style.display = recording ? '' : 'none';
    });
}

function hideSessionHeader() {
    const bar = document.getElementById('recording-bar');
    if (bar) bar.style.display = 'none';
    const legend = document.getElementById('header-legend');
    if (legend) { legend.style.display = 'none'; legend.innerHTML = ''; }
    const speakers = document.getElementById('header-speaker-names');
    if (speakers) speakers.style.display = 'none';
}

// --- Type emoji map + legend ---

const typeEmojis = {
    claim:         '💬',
    response:      '↩️',
    question:      '❓',
    agreement:     '✅',
    rebuttal:      '⚔️',
    tangent:       '🌀',
    clarification: '🔍',
    evidence:      '📎',
};

function typeEmoji(type) {
    return typeEmojis[type] || '💬';
}

function resolveSpeaker(raw) {
    if (!raw) return '';
    for (const [id, name] of Object.entries(speakerNames)) {
        if (id === raw || name === raw) return name;
    }
    const normalized = raw.toLowerCase().replace(/\s+/g, '_');
    if (speakerNames[normalized]) return speakerNames[normalized];
    return raw;
}

function renderLegend() {
    const legend = document.getElementById('header-legend');
    if (!legend) return;
    if (legend.children.length > 0) { legend.style.display = ''; return; }
    legend.style.display = '';
    legend.innerHTML = `<span class="legend-trigger">🏷️ Legend</span><div class="legend-dropdown">${
        Object.entries(typeEmojis)
            .map(([type, emoji]) => `<span class="legend-item">${emoji} ${type}</span>`)
            .join('')
    }</div>`;
}

// --- Stable tree rendering with DOM diffing ---

let statementIdCounter = 0;

function assignIds(statements) {
    for (const s of statements) {
        if (!s._id) s._id = 'stmt-' + (++statementIdCounter);
        if (s.children) assignIds(s.children);
    }
}

function renderArgumentTree() {
    const section = document.getElementById('argument-section');
    const tree = document.getElementById('argument-tree');
    if (!section || !tree) return;
    if (analyzedStatements.length === 0) return;

    section.style.display = '';
    renderLegend();
    assignIds(analyzedStatements);
    diffChildren(tree, analyzedStatements, 0);

    // Delegated hover for cross-highlight (set up once)
    if (!tree._hoverBound) {
        tree._hoverBound = true;
        let activeIdx = null;
        tree.addEventListener('mouseover', (e) => {
            // Find the innermost .statement with data-msg-idx
            const stmt = e.target.closest('.statement[data-msg-idx]');
            const idx = stmt ? stmt.dataset.msgIdx : null;
            if (idx === activeIdx) return;
            if (activeIdx) unhighlightMsg(activeIdx);
            activeIdx = idx;
            if (idx) highlightMsg(idx);
        });
        tree.addEventListener('mouseleave', () => {
            if (activeIdx) { unhighlightMsg(activeIdx); activeIdx = null; }
        });
    }
}

function diffChildren(container, statements, depth) {
    const existingById = {};
    for (const child of Array.from(container.children)) {
        const id = child.dataset.stmtId;
        if (id) existingById[id] = child;
    }

    const desiredIds = new Set(statements.map(s => s._id));

    for (const [id, el] of Object.entries(existingById)) {
        if (!desiredIds.has(id)) {
            el.classList.add('stmt-removing');
            setTimeout(() => el.remove(), 300);
        }
    }

    let prevEl = null;
    for (const s of statements) {
        let el = existingById[s._id];
        if (!el) {
            el = createStatementEl(s, depth);
            el.classList.add('stmt-entering');
            requestAnimationFrame(() => el.classList.remove('stmt-entering'));

            if (prevEl && prevEl.nextSibling) {
                container.insertBefore(el, prevEl.nextSibling);
            } else if (!prevEl) {
                container.insertBefore(el, container.firstChild);
            } else {
                container.appendChild(el);
            }
        } else {
            updateStatementEl(el, s, depth);
        }

        if (s.children && s.children.length > 0) {
            let childrenContainer = el.querySelector(':scope > details > .children');
            if (!childrenContainer) {
                rebuildAsParent(el, s, depth);
                childrenContainer = el.querySelector(':scope > details > .children');
            }
            if (childrenContainer) {
                assignIds(s.children);
                diffChildren(childrenContainer, s.children, depth + 1);
            }
        }

        prevEl = el;
    }
}

function createStatementEl(s, depth) {
    const div = document.createElement('div');
    div.dataset.stmtId = s._id;
    populateStatementEl(div, s, depth);
    return div;
}

function speakerIdFromName(raw) {
    if (!raw) return null;
    const lower = raw.toLowerCase().trim();
    for (const [id, name] of Object.entries(speakerNames)) {
        if (id === raw || name === raw) return id;
        if (id === lower || name.toLowerCase() === lower) return id;
    }
    return null;
}

function speakerBgColor(speaker) {
    const colorMap = getColorMap();
    // Direct ID match first
    if (colorMap[speaker]) return colorMap[speaker];
    // Resolve name to ID (case-insensitive)
    const id = speakerIdFromName(speaker);
    if (id && colorMap[id]) return colorMap[id];
    // Fallback: hash to a color
    const s = (speaker || '').toLowerCase();
    const idx = Array.from(s).reduce((a, c) => a + c.charCodeAt(0), 0);
    return speakerColors[idx % speakerColors.length];
}

// Generate timestamps based on word count (~150 words/min)
function assignWordBasedTimestamps(messages) {
    if (!messages || !messages.length) return;
    // Skip if already have real timestamps
    if (messages.some(m => m.start_ms != null)) return;
    let runningMs = 0;
    for (const msg of messages) {
        msg.start_ms = runningMs;
        const words = (msg.text || '').split(/\s+/).filter(w => w).length;
        const durationMs = Math.max(2000, Math.round((words / 2.5) * 1000)); // ~150wpm, min 2s
        msg.end_ms = runningMs + durationMs;
        runningMs += durationMs + 500; // 500ms pause between speakers
    }
}

function formatMs(ms) {
    const totalSecs = Math.floor(ms / 1000);
    const h = Math.floor(totalSecs / 3600);
    const m = Math.floor((totalSecs % 3600) / 60);
    const s = totalSecs % 60;
    if (h > 0) return `${h}:${String(m).padStart(2,'0')}:${String(s).padStart(2,'0')}`;
    return `${m}:${String(s).padStart(2,'0')}`;
}

function highlightMsg(idx) {
    document.querySelectorAll('.argument-tree, .chat-messages').forEach(c => c.classList.add('has-highlight'));
    document.querySelectorAll(`[data-msg-idx="${idx}"]`).forEach(el => {
        el.classList.add('msg-highlight-self');
        if (!isElementVisible(el)) {
            el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
    });
}
function unhighlightMsg(idx) {
    document.querySelectorAll(`[data-msg-idx="${idx}"]`).forEach(el => el.classList.remove('msg-highlight-self'));
    document.querySelectorAll('.argument-tree, .chat-messages').forEach(c => c.classList.remove('has-highlight'));
}
function isElementVisible(el) {
    const container = el.closest('.section-body') || el.parentElement;
    if (!container) return true;
    const cRect = container.getBoundingClientRect();
    const eRect = el.getBoundingClientRect();
    return eRect.top >= cRect.top && eRect.bottom <= cRect.bottom;
}

function getMsgInfo(s) {
    const idx = s.msg_index;
    if (idx == null || !diarizeData || !diarizeData.messages) return { idx: '', startMs: null };
    // Find message by position (1-based), falling back to array index
    const msg = diarizeData.messages.find(m => m.position === idx) || diarizeData.messages[idx - 1];
    return { idx: String(idx), startMs: msg ? msg.start_ms : null };
}

function populateStatementEl(div, s, depth) {
    const typeClass = s.type || 'claim';
    const flagged = (s.fact_check ? ` flagged flagged-${s.fact_check.verdict}` : '') + (s.fallacy ? ' has-fallacy' : '');
    div.className = `statement depth-${depth} type-${typeClass}${flagged}`;
    div.style.setProperty('--speaker-bg', speakerBgColor(s.speaker));

    const emoji = typeEmoji(typeClass);
    const speaker = escapeHtml(resolveSpeaker(s.speaker));
    const text = escapeHtml(s.text || '');
    const flagsHtml = (s.fact_check ? renderFactCheckHtml(s.fact_check) : '') + (s.fallacy ? renderFallacyHtml(s.fallacy) : '');
    const match = getMsgInfo(s);
    const idx = match.idx;
    const tsHtml = match.startMs != null ? `<span class="msg-time">${formatMs(match.startMs)}</span>` : '';
    if (idx) {
        div.dataset.msgIdx = idx;
    }
    const hasChildren = s.children && s.children.length > 0;

    if (hasChildren) {
        const count = countDescendants(s);
        div.innerHTML = `<details open>
            <summary>${tsHtml}<span class="type-badge">${emoji}</span> <span class="speaker">${speaker}:</span> ${text} <span class="child-count">(${count})</span>${flagsHtml}</summary>
            <div class="children"></div>
        </details>`;
    } else {
        div.innerHTML = `<div class="leaf">${tsHtml}<span class="type-badge">${emoji}</span> <span class="speaker">${speaker}:</span> ${text}${flagsHtml}</div>`;
    }
}

function updateStatementEl(el, s, depth) {
    const key = `${s.type}|${s.speaker}|${s.text}|${JSON.stringify(s.fact_check || '')}|${JSON.stringify(s.fallacy || '')}`;
    if (el.dataset.contentKey === key) return;
    el.dataset.contentKey = key;

    const typeClass = s.type || 'claim';
    const flagged = (s.fact_check ? ` flagged flagged-${s.fact_check.verdict}` : '') + (s.fallacy ? ' has-fallacy' : '');
    el.className = `statement depth-${depth} type-${typeClass}${flagged}`;
    el.style.setProperty('--speaker-bg', speakerBgColor(s.speaker));

    const emoji = typeEmoji(typeClass);
    const speaker = escapeHtml(resolveSpeaker(s.speaker));
    const text = escapeHtml(s.text || '');
    const flagsHtml = (s.fact_check ? renderFactCheckHtml(s.fact_check) : '') + (s.fallacy ? renderFallacyHtml(s.fallacy) : '');
    const match = getMsgInfo(s);
    const idx = match.idx;
    const tsHtml = match.startMs != null ? `<span class="msg-time">${formatMs(match.startMs)}</span>` : '';

    const summary = el.querySelector(':scope > details > summary');
    if (summary) {
        const count = countDescendants(s);
        summary.innerHTML = `${tsHtml}<span class="type-badge">${emoji}</span> <span class="speaker">${speaker}:</span> ${text} <span class="child-count">(${count})</span>${flagsHtml}`;
    } else {
        const leaf = el.querySelector(':scope > .leaf');
        if (leaf) {
            leaf.innerHTML = `${tsHtml}<span class="type-badge">${emoji}</span> <span class="speaker">${speaker}:</span> ${text}${flagsHtml}`;
        }
    }
}

function rebuildAsParent(el, s, depth) {
    const typeClass = s.type || 'claim';
    const emoji = typeEmoji(typeClass);
    const speaker = escapeHtml(resolveSpeaker(s.speaker));
    const text = escapeHtml(s.text || '');
    const flagsHtml = (s.fact_check ? renderFactCheckHtml(s.fact_check) : '') + (s.fallacy ? renderFallacyHtml(s.fallacy) : '');
    const count = countDescendants(s);
    el.innerHTML = `<details open>
        <summary><span class="type-badge">${emoji}</span> <span class="speaker">${speaker}:</span> ${text} <span class="child-count">(${count})</span>${flagsHtml}</summary>
        <div class="children"></div>
    </details>`;
}

function renderFactCheckHtml(fc) {
    const verdict = escapeHtml(fc.verdict);
    const correction = escapeHtml(fc.correction);
    const searchURL = 'https://www.google.com/search?q=' + encodeURIComponent(fc.search_query || '');
    return `<div class="fact-check verdict-${verdict}">
        <span class="fact-verdict">⚠ ${verdict}</span>
        <span class="fact-correction">${correction}</span>
        <a href="${searchURL}" target="_blank" rel="noopener" class="fact-source">verify ↗</a>
    </div>`;
}

function renderFallacyHtml(f) {
    const name = escapeHtml(f.name);
    const explanation = escapeHtml(f.explanation);
    const searchURL = 'https://www.google.com/search?q=' + encodeURIComponent(f.name + ' logical fallacy');
    return `<div class="fallacy-flag">
        <span class="fallacy-name">🧠 ${name}</span>
        <span class="fallacy-explanation">${explanation}</span>
        <a href="${searchURL}" target="_blank" rel="noopener" class="fact-source">learn more ↗</a>
    </div>`;
}

function countDescendants(s) {
    let count = (s.children || []).length;
    for (const c of (s.children || [])) count += countDescendants(c);
    return count;
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

    const section = document.getElementById('header-speaker-names');
    const list = document.getElementById('header-speaker-list');
    if (section && list) {
        section.style.display = '';

        // Compute per-speaker stats
        const speakerWords = {};
        const speakerMsgCount = {};
        if (diarizeData && diarizeData.messages) {
            for (const msg of diarizeData.messages) {
                const wc = (msg.text || '').split(/\s+/).filter(w => w).length;
                speakerWords[msg.speaker] = (speakerWords[msg.speaker] || 0) + wc;
                speakerMsgCount[msg.speaker] = (speakerMsgCount[msg.speaker] || 0) + 1;
            }
        }

        let html = '';
        for (const id of Object.keys(speakerNames)) {
            const color = speakerBgColor(id);
            const isAnon = speakerAutoGen[id];
            const name = speakerNames[id];
            const words = speakerWords[id] || 0;
            const msgs = speakerMsgCount[id] || 0;
            // Compute speaking time
            let totalMs = 0;
            if (diarizeData && diarizeData.messages) {
                const allMsgs = diarizeData.messages;
                for (let mi = 0; mi < allMsgs.length; mi++) {
                    if (allMsgs[mi].speaker !== id) continue;
                    const startMs = allMsgs[mi].start_ms;
                    if (startMs == null) continue;
                    const nextIdx = mi + 1;
                    const endMs = allMsgs[mi].end_ms != null ? allMsgs[mi].end_ms
                        : (nextIdx < allMsgs.length && allMsgs[nextIdx].start_ms != null ? allMsgs[nextIdx].start_ms : startMs + 5000);
                    totalMs += endMs - startMs;
                }
            }
            const speakTimeHtml = ` · ${formatMs(totalMs)}`;
            html += `
                <div class="speaker-input" style="--speaker-color: ${color}">
                    <span class="speaker-dot" style="background: ${color}"></span>`;
            if (isAnon) {
                html += `<span class="speaker-anon-tag" title="Anonymous — click name to rename">anon</span>`;
            } else if (!isAnon && speakerDbIds[id]) {
                html += `<a class="speaker-link" href="${BASE_PATH}/speaker/${encodeURIComponent(name)}" onclick="event.preventDefault(); loadSpeakerPage('${escapeHtml(name)}')" title="View speaker page">↗</a>`;
            }
            html += `
                    <input type="text" value="${escapeHtml(name)}"
                           onchange="renameSpeaker('${id}', this.value)"
                           class="${isAnon ? 'anon-name' : ''}"
                           data-speaker="${id}">
                    <span class="speaker-stats">${words}w · ${msgs} msgs${speakTimeHtml}</span>
                </div>`;
        }
        list.innerHTML = html;
    }

    const container = document.getElementById('chat-messages');
    if (!container) return;

    let html = '';
    for (let i = 0; i < diarizeData.messages.length; i++) {
        const msg = diarizeData.messages[i];
        const name = speakerNames[msg.speaker] || msg.speaker;
        const color = speakerBgColor(msg.speaker);
        const tsHtml = msg.start_ms != null ? `<span class="msg-time">${formatMs(msg.start_ms)}</span>` : '';
        html += `
            <div class="chat-msg" data-speaker="${msg.speaker}" data-msg-idx="${msg.position || i + 1}" style="--speaker-bg: ${color}"
>
                ${tsHtml}
                <span class="chat-speaker">${escapeHtml(name)}</span>
                <span class="chat-text">${escapeHtml(msg.text)}</span>
            </div>`;
    }
    container.innerHTML = html;
    container.scrollTop = container.scrollHeight;

    // Delegated hover for cross-highlight (set up once)
    if (!container._hoverBound) {
        container._hoverBound = true;
        let activeIdx = null;
        container.addEventListener('mouseover', (e) => {
            const msg = e.target.closest('.chat-msg[data-msg-idx]');
            const idx = msg ? msg.dataset.msgIdx : null;
            if (idx === activeIdx) return;
            if (activeIdx) unhighlightMsg(activeIdx);
            activeIdx = idx;
            if (idx) highlightMsg(idx);
        });
        container.addEventListener('mouseleave', () => {
            if (activeIdx) { unhighlightMsg(activeIdx); activeIdx = null; }
        });
    }
}

function renameSpeaker(id, newName) {
    const oldName = speakerNames[id];
    speakerNames[id] = newName;
    speakerAutoGen[id] = false;  // renamed = no longer anonymous
    document.querySelectorAll(`.chat-msg[data-speaker="${id}"] .chat-speaker`).forEach(el => {
        el.textContent = newName;
    });
    if (analyzedStatements.length > 0) renderArgumentTree();
    renderChatMessages();  // re-render header to update anon/link state

    // Persist: update speakers table + diarization for current transcript
    if (oldName && oldName !== newName) {
        fetch(BASE_PATH + '/api/speakers/' + encodeURIComponent(oldName), {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name: newName })
        }).catch(e => console.warn('Failed to persist speaker rename:', e));
    }
    if (currentSlug) {
        fetch(BASE_PATH + '/api/transcripts/' + encodeURIComponent(currentSlug) + '/speakers', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ speakers: speakerNames, speaker_auto_gen: speakerAutoGen })
        }).catch(e => console.warn('Failed to persist transcript speakers:', e));
    }
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

    // Remove YouTube embed if present
    const ytEmbed = document.getElementById('yt-live-embed');
    if (ytEmbed) ytEmbed.remove();

    // Hide recording controls but keep speaker names and legend visible
    const bar = document.getElementById('recording-bar');
    if (bar) {
        bar.querySelectorAll('.record-dot, .record-time, .btn-stop').forEach(el => {
            el.style.display = 'none';
        });
    }

    if (mediaRecorder) {
        mediaRecorder.stop();
        clearInterval(recordTimer);
    }

    if (recordedChunks.length > 0) {
        const blob = new Blob(recordedChunks, { type: 'audio/webm' });
        const form = new FormData();
        form.append('audio', blob, 'recording.webm');

        fetch(BASE_PATH + '/api/transcribe', { method: 'POST', body: form })
            .then(r => r.json())
            .then(async data => {
                fullTranscript = (data.text || '').trim() || fullTranscript;
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

    const session = document.querySelector('.live-session');
    if (!session) return;

    const existing = session.querySelector('.analyze-form');
    if (existing) existing.remove();

    const formDiv = document.createElement('div');
    formDiv.className = 'analyze-form';
    formDiv.innerHTML = `
        <div class="action-row">
            <button class="btn" onclick="runFinalAnalysis()">Re-analyze</button>
            <button class="btn btn-secondary" onclick="showInputs()">New</button>
        </div>`;
    session.appendChild(formDiv);

    const currentTranscript = buildTranscriptText();
    if (currentTranscript !== lastAnalyzedTranscript) {
        pendingAnalyze = true;
        analyzeAsync(currentTranscript).finally(() => { pendingAnalyze = false; });
    }
}

function runFinalAnalysis() {
    const transcript = buildTranscriptText();
    analyzedStatements = [];
    lastAnalyzedTranscript = '';
    pendingAnalyze = true;
    analyzeAsync(transcript, true).finally(() => { pendingAnalyze = false; });
}

// --- Audio upload → transcribe → diarize ---

async function submitAudioForDiarize(form) {
    hideInputs();
    lastAnalyzedTranscript = '';
    analyzedStatements = [];
    await createSession();
    document.getElementById('spinner').classList.add('htmx-request');
    showSessionHeader(false);
    initLiveSession('Transcribing...');

    try {
        const resp = await fetch(BASE_PATH + '/api/transcribe', { method: 'POST', body: form });
        const data = await resp.json();
        document.getElementById('spinner').classList.remove('htmx-request');

        const text = (data.text || '').trim();
        if (!text) {
            document.getElementById('result').innerHTML = '<div class="error">No speech detected</div>';
            return;
        }

        fullTranscript = text;
        diarizeData = null;
        speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();

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
    lastAnalyzedTranscript = '';
    analyzedStatements = [];
    fullTranscript = text;
    diarizeData = null;
    speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();
    await createSession();

    showSessionHeader(false);
    initLiveSession('Identifying speakers...');

    await diarizeAsync(text);
    showFinalView();
}

// --- Conversation browser ---

async function loadDiscovery() {
    const discovery = document.getElementById('discovery');
    if (!discovery) return;

    const [speakersResp, convosResp] = await Promise.all([
        fetch(BASE_PATH + '/api/speakers').catch(() => null),
        fetch(BASE_PATH + '/api/transcripts').catch(() => null),
    ]);

    const speakers = speakersResp ? await speakersResp.json() : [];
    const convos = convosResp ? await convosResp.json() : [];

    if ((!speakers || speakers.length === 0) && (!convos || convos.length === 0)) return;

    discovery.style.display = '';

    // Speakers column
    const speakersList = document.getElementById('speakers-list');
    const speakersCol = document.getElementById('discovery-speakers');
    if (speakers && speakers.length > 0) {
        speakersList.innerHTML = speakers.slice(0, 10).map(s => `
            <a class="speaker-chip" href="${BASE_PATH}/speaker/${encodeURIComponent(s.name)}" onclick="event.preventDefault(); loadSpeakerPage('${escapeHtml(s.name)}')">
                <span class="speaker-chip-name">${escapeHtml(s.name)}</span>
                <span class="speaker-chip-meta">${s.conversation_count} convo${s.conversation_count !== 1 ? 's' : ''} · ${s.claim_count} claims</span>
            </a>
        `).join('');
    } else {
        speakersCol.style.display = 'none';
    }

    // Conversations column
    const convosList = document.getElementById('conversations-list');
    const convosCol = document.getElementById('discovery-conversations');
    if (convos && convos.length > 0) {
        convosList.innerHTML = convos.slice(0, 10).map(t => {
            const date = new Date(t.created_at).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
            const slug = t.slug || t.id;
            const title = t.title ? t.title.substring(0, 60) : 'Untitled';
            return `
                <div class="conversation-item" onclick="loadConversation('${escapeHtml(slug)}')">
                    <div class="conversation-title">${escapeHtml(slug)}  —  ${escapeHtml(title)}</div>
                    <div class="conversation-meta">${date}</div>
                </div>`;
        }).join('');
    } else {
        convosCol.style.display = 'none';
    }
}

async function loadSpeakersPage() {
    hideInputs();
    currentSlug = null;
    history.pushState({ page: 'speakers' }, '', BASE_PATH + '/speakers');

    document.getElementById('result').innerHTML = '<div class="list-page"><h2>Speakers</h2><p class="text-dim">Loading…</p></div>';

    try {
        const resp = await fetch(BASE_PATH + '/api/speakers');
        const speakers = await resp.json();

        let html = '<div class="list-page"><h2>Speakers</h2>';
        if (!speakers || speakers.length === 0) {
            html += '<p class="text-dim">No speakers yet. Start a conversation!</p>';
        } else {
            html += '<div class="list-items">';
            for (const s of speakers) {
                html += `
                    <a class="list-item" href="${BASE_PATH}/speaker/${encodeURIComponent(s.name)}" onclick="event.preventDefault(); loadSpeakerPage('${escapeHtml(s.name)}')">
                        <span class="list-item-name">${escapeHtml(s.name)}</span>
                        <span class="list-item-meta">${s.conversation_count} convo${s.conversation_count !== 1 ? 's' : ''} · ${s.claim_count} claims</span>
                    </a>`;
            }
            html += '</div>';
        }
        html += '</div>';
        document.getElementById('result').innerHTML = html;
    } catch (e) {
        document.getElementById('result').innerHTML = '<div class="error">Failed to load speakers</div>';
    }
}

async function loadConversationsPage() {
    hideInputs();
    currentSlug = null;
    history.pushState({ page: 'conversations' }, '', BASE_PATH + '/conversations');

    document.getElementById('result').innerHTML = '<div class="list-page"><h2>Conversations</h2><p class="text-dim">Loading…</p></div>';

    try {
        const resp = await fetch(BASE_PATH + '/api/transcripts');
        const convos = await resp.json();

        let html = '<div class="list-page"><h2>Conversations</h2>';
        if (!convos || convos.length === 0) {
            html += '<p class="text-dim">No conversations yet. Record or paste one!</p>';
        } else {
            html += '<div class="list-items">';
            for (const t of convos) {
                const date = new Date(t.created_at).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
                const slug = t.slug || t.id;
                const title = t.title ? t.title.substring(0, 80) : 'Untitled';
                html += `
                    <div class="list-item" onclick="loadConversation('${escapeHtml(slug)}')">
                        <div>
                            <span class="list-item-name">${escapeHtml(slug)}</span>
                            <span class="list-item-title">${escapeHtml(title)}</span>
                        </div>
                        <span class="list-item-meta">${date}</span>
                    </div>`;
            }
            html += '</div>';
        }
        html += '</div>';
        document.getElementById('result').innerHTML = html;
    } catch (e) {
        document.getElementById('result').innerHTML = '<div class="error">Failed to load conversations</div>';
    }
}

async function loadSpeakerPage(name) {
    hideInputs();
    currentSlug = null;
    history.pushState({ speaker: name }, '', BASE_PATH + '/speaker/' + encodeURIComponent(name));

    document.getElementById('result').innerHTML = '<div class="speaker-page"><h2>' + escapeHtml(name) + '</h2><p class="text-dim">Loading…</p></div>';

    try {
        const resp = await fetch(BASE_PATH + '/api/speakers/' + encodeURIComponent(name));
        const data = await resp.json();
        const convos = data.conversations || [];

        let html = `<div class="speaker-page">
            <h2>${escapeHtml(name)}</h2>
            <p class="text-dim">${convos.length} conversation${convos.length !== 1 ? 's' : ''}</p>`;

        if (convos.length === 0) {
            html += '<p>No conversations found.</p>';
        } else {
            html += '<div class="speaker-convos">';
            for (const c of convos) {
                const date = new Date(c.created_at).toLocaleDateString('en-US', {
                    month: 'short', day: 'numeric', year: 'numeric'
                });
                const title = c.title ? c.title.substring(0, 80) : 'Untitled';
                html += `
                    <div class="conversation-item" onclick="loadConversation('${escapeHtml(c.slug)}')">
                        <div class="conversation-title">${escapeHtml(c.slug)}  —  ${escapeHtml(title)}</div>
                        <div class="conversation-meta">${date} · ${c.claim_count} claims</div>
                    </div>`;
            }
            html += '</div>';
        }

        html += '</div>';
        document.getElementById('result').innerHTML = html;
    } catch (e) {
        document.getElementById('result').innerHTML = '<div class="error">Failed to load speaker: ' + e.message + '</div>';
    }
}

async function loadConversation(slugOrId) {
    hideInputs();
    lastAnalyzedTranscript = '';
    analyzedStatements = [];
    statementIdCounter = 0;
    diarizeData = null;
    speakerNames = {}; speakerAutoGen = {}; speakerDbIds = {}; usedAnonNames.clear();

    currentSlug = slugOrId;
    if (window.location.pathname !== BASE_PATH + '/' + slugOrId) {
        history.pushState({ slug: slugOrId }, '', BASE_PATH + '/' + slugOrId);
    }

    initLiveSession('Loading conversation...');
    showSessionHeader(false);

    try {
        const resp = await fetch(BASE_PATH + '/api/transcripts/' + encodeURIComponent(slugOrId));
        const data = await resp.json();

        // Reconstruct transcript text from utterances
        if (data.messages && data.messages.length > 0) {
            fullTranscript = data.messages.map(m => {
                const name = (data.speakers && data.speakers[m.speaker]) || m.speaker;
                return name + ': ' + m.text;
            }).join('\n');
        } else {
            fullTranscript = '';
        }
        currentSourceURL = data.transcript.source_url || '';
        if (data.transcript.title) updateConversationTitle(data.transcript.title, data.transcript.source_url);

        // Restore diarization
        if (data.messages && data.messages.length > 0) {
            diarizeData = {
                speakers: data.speakers || {},
                messages: data.messages
            };
            assignWordBasedTimestamps(diarizeData.messages);
            // Build speakerNames and auto_generated info
            for (const [sid, name] of Object.entries(data.speakers || {})) {
                speakerNames[sid] = name || sid.replace('_', ' ').replace(/\b\w/g, c => c.toUpperCase());
            }
            if (data.speaker_info) {
                for (const [sid, info] of Object.entries(data.speaker_info)) {
                    speakerAutoGen[sid] = !!info.auto_generated;
                    if (info.id) speakerDbIds[sid] = info.id;
                    if (info.name) speakerNames[sid] = info.name;
                }
            }
            renderChatMessages();
        } else {
            // No diarization data — just show raw text
            const chatEl = document.getElementById('chat-messages');
            if (chatEl) {
                chatEl.innerHTML = `<div class="chat-msg"><span class="chat-text">${escapeHtml(fullTranscript)}</span></div>`;
            }
        }

        // Restore claim tree
        if (data.statements && data.statements.length > 0) {
            analyzedStatements = data.statements;
            lastAnalyzedTranscript = buildTranscriptText();
            renderArgumentTree();
        }

        showFinalView();
    } catch (e) {
        document.getElementById('result').innerHTML = '<div class="error">Failed to load conversation: ' + e.message + '</div>';
    }
}

// --- Utilities ---

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function updateRecordTime() {
    recordTimer = setInterval(() => {
        const elapsed = Math.floor((Date.now() - recordStart) / 1000);
        const m = String(Math.floor(elapsed / 60)).padStart(2, '0');
        const s = String(elapsed % 60).padStart(2, '0');
        const time = m + ':' + s;
        const el = document.getElementById('record-time');
        const bar = document.getElementById('recording-bar-time');
        if (el) el.textContent = time;
        if (bar) bar.textContent = time;
    }, 1000);
}

// Handle slug URLs and load conversation list on page load
document.addEventListener('DOMContentLoaded', () => {
    loadDiscovery();
    // Check URL for routing
    const path = window.location.pathname.replace(BASE_PATH, '').replace(/^\/+/, '');
    if (path === 'speakers') {
        loadSpeakersPage();
    } else if (path === 'conversations') {
        loadConversationsPage();
    } else if (path.startsWith('speaker/')) {
        const name = decodeURIComponent(path.substring(8));
        loadSpeakerPage(name);
    } else if (path && !path.includes('/') && !path.includes('.')) {
        loadConversation(path);
    }
});

// Handle browser back/forward
window.addEventListener('popstate', (e) => {
    if (e.state && e.state.slug) {
        loadConversation(e.state.slug);
    } else if (e.state && e.state.speaker) {
        loadSpeakerPage(e.state.speaker);
    } else if (e.state && e.state.page === 'speakers') {
        loadSpeakersPage();
    } else if (e.state && e.state.page === 'conversations') {
        loadConversationsPage();
    } else {
        showInputs();
        currentSlug = null;
    }
});
