import React, { useState, useRef } from 'react';
import { getBasePath } from '../api';
import * as api from '../api';
import { useSession } from '../context/SessionContext';
import { useSpeakers } from '../context/SpeakerContext';
import { useRecording } from '../hooks/useRecording';
import { assignWordBasedTimestamps } from '../utils/timestamps';
import DiscoverySection from './DiscoverySection';
import AppHeader from './AppHeader';

function extractYouTubeId(url: string): string {
  const m = url.match(/(?:v=|youtu\.be\/)([a-zA-Z0-9_-]{11})/);
  return m ? m[1] : '';
}

export default function HomePage() {
  const {
    setView,
    createNewSession,
    setDiarizeData,
    setFullTranscript,
    setAnalyzedStatements,
    setIsRecording,
    setSourceURL,
    setSourceTitle,
    diarizeAsync,
    analyzeAsync,
    pendingAnalyze,
    setShowFinal,
    lastAnalyzedTranscript,
    pendingYouTubeRecord,
  } = useSession();
  const { resetSpeakers, setSpeakerNames, setSpeakerAutoGen, pickAnonName, initSpeakersFromDiarize } = useSpeakers();
  const { startRecording, startYouTubeRecording } = useRecording();

  const [ytUrl, setYtUrl] = useState('');
  const [ytStatus, setYtStatus] = useState('');
  const [ytStatusClass, setYtStatusClass] = useState('');
  const [pasteText, setPasteText] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const goToSession = () => setView('session');

  const handleRecord = async (mode: 'mic' | 'tab') => {
    resetSpeakers();
    setFullTranscript('');
    setDiarizeData(null);
    setAnalyzedStatements([]);
    lastAnalyzedTranscript.current = '';
    setShowFinal(false);
    await createNewSession();
    goToSession();
    const ok = await startRecording(mode);
    if (!ok) setView('home');
  };

  const handleYouTube = async (e: React.FormEvent) => {
    e.preventDefault();
    const url = ytUrl.trim();
    if (!url) return;
    const videoId = extractYouTubeId(url);
    if (!videoId) {
      setYtStatus('Invalid YouTube URL');
      setYtStatusClass('error');
      return;
    }
    resetSpeakers();
    setFullTranscript('');
    setDiarizeData(null);
    setAnalyzedStatements([]);
    lastAnalyzedTranscript.current = '';
    setSourceURL(url);
    setShowFinal(false);
    pendingYouTubeRecord.current = true;
    await createNewSession();
    goToSession();
  };

  const handlePaste = async (e: React.FormEvent) => {
    e.preventDefault();
    const text = pasteText.trim();
    if (!text) return;
    resetSpeakers();
    setFullTranscript(text);
    setDiarizeData(null);
    setAnalyzedStatements([]);
    lastAnalyzedTranscript.current = '';
    setShowFinal(false);
    await createNewSession();
    goToSession();
    await diarizeAsync(text);
    setShowFinal(true);
  };

  const handleSample = async () => {
    setYtStatus('Generating sample conversation...');
    setYtStatusClass('loading');
    try {
      const data = await api.fetchSample();
      setYtStatus('');
      resetSpeakers();
      setSourceURL(data.url || '');
      const dd = { speakers: data.speakers, messages: data.messages };
      assignWordBasedTimestamps(dd.messages);
      setDiarizeData(dd);
      setFullTranscript(data.text);
      // Set speaker names
      const names: Record<string, string> = {};
      const autoGen: Record<string, boolean> = {};
      for (const [id, name] of Object.entries(data.speakers)) {
        names[id] = name || pickAnonName();
        if (!name) autoGen[id] = true;
      }
      setSpeakerNames(names);
      setSpeakerAutoGen(autoGen);
      setAnalyzedStatements([]);
      lastAnalyzedTranscript.current = '';
      await createNewSession();
      if (data.title) setSourceTitle(data.title);
      setShowFinal(true);
      goToSession();
      // Run analysis
      const transcript = dd.messages.map((m) => `${names[m.speaker] || m.speaker}: ${m.text}`).join('\n');
      pendingAnalyze.current = true;
      analyzeAsync(transcript).finally(() => { pendingAnalyze.current = false; });
    } catch (e: any) {
      setYtStatus('Failed: ' + e.message);
      setYtStatusClass('error');
    }
  };

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    const form = new FormData();
    form.append('audio', file);
    resetSpeakers();
    setFullTranscript('');
    setDiarizeData(null);
    setAnalyzedStatements([]);
    lastAnalyzedTranscript.current = '';
    setShowFinal(false);
    await createNewSession();
    goToSession();
    try {
      const data = await api.transcribeAudio(form);
      const text = (data.text || '').trim();
      if (!text) return;
      setFullTranscript(text);
      await diarizeAsync(text);
      setShowFinal(true);
    } catch {}
    e.target.value = '';
  };

  return (
    <div className="container">
      <AppHeader />
      <div className="input-section" id="input-section">
        <div className="input-group">
          <label className="input-group-label">Record live</label>
          <div className="input-row">
            <button className="btn btn-record" onClick={() => handleRecord('mic')}>
              <span className="record-dot"></span> Microphone
            </button>
            <button className="btn btn-record" onClick={() => handleRecord('tab')}>
              <span className="record-dot"></span> Tab Audio
            </button>
          </div>
        </div>

        <div className="input-group">
          <label className="input-group-label">From YouTube</label>
          <form onSubmit={handleYouTube}>
            <div className="input-row">
              <input
                type="text"
                placeholder="https://youtube.com/watch?v=..."
                className="yt-url-input"
                value={ytUrl}
                onChange={(e) => setYtUrl(e.target.value)}
              />
              <button type="submit" className="btn">Analyze</button>
            </div>
          </form>
          {ytStatus && <div className={`yt-status ${ytStatusClass}`}>{ytStatus}</div>}
        </div>

        <div className="input-group">
          <label className="input-group-label">From text</label>
          <form onSubmit={handlePaste}>
            <textarea
              ref={textareaRef}
              rows={6}
              placeholder="Paste a conversation transcript..."
              value={pasteText}
              onChange={(e) => setPasteText(e.target.value)}
            />
            <div className="input-row input-row-compact">
              <button type="submit" className="btn">Analyze</button>
              <label className="btn btn-secondary btn-upload">
                <input type="file" accept="audio/*,video/*" onChange={handleUpload} hidden />
                Upload Audio
              </label>
              <button type="button" className="btn btn-secondary" onClick={handleSample}>
                Try a sample
              </button>
            </div>
          </form>
        </div>

        <DiscoverySection />
      </div>
    </div>
  );
}
