import React, { useEffect, useCallback, useRef } from 'react';
import { getBasePath, getTranscript } from '../api';
import { useSession } from '../context/SessionContext';
import { useSpeakers } from '../context/SpeakerContext';
import { useHighlight } from '../hooks/useHighlight';
import { useRecording } from '../hooks/useRecording';
import { assignWordBasedTimestamps } from '../utils/timestamps';
import AppHeader from './AppHeader';
import SessionHeader from './SessionHeader';
import TranscriptPanel from './TranscriptPanel';
import ArgumentTree from './ArgumentTree';
import YouTubeEmbed from './YouTubeEmbed';

export default function SessionPage() {
  const {
    slug,
    isRecording,
    diarizeData,
    analyzedStatements,
    setDiarizeData,
    setFullTranscript,
    setAnalyzedStatements,
    sourceURL,
    setSourceURL,
    setSourceTitle,
    showFinal,
    setShowFinal,
    buildTranscriptText,
    analyzeAsync,
    pendingAnalyze,
    lastAnalyzedTranscript,
    setView,
    pendingYouTubeRecord,
  } = useSession();

  const {
    speakerNames,
    speakerAutoGen,
    speakerDbIds,
    setSpeakerNames,
    setSpeakerAutoGen,
    setSpeakerDbIds,
    resetSpeakers,
  } = useSpeakers();

  const { highlightIdx, pinnedIdx, onHover, onPin } = useHighlight();
  const { stopRecording, recordTime, startYouTubeRecording } = useRecording();

  // Start YouTube recording only when explicitly requested from HomePage
  useEffect(() => {
    if (!pendingYouTubeRecord.current || isRecording || !sourceURL) return;
    const m = sourceURL.match(/(?:v=|youtu\.be\/)([a-zA-Z0-9_-]{11})/);
    if (!m) return;
    pendingYouTubeRecord.current = false;
    startYouTubeRecording(m[1], sourceURL).then((ok) => {
      if (!ok) {
        setView('home');
      }
    });
  }, [sourceURL, isRecording]);

  // Load existing conversation if slug set but no diarize data
  useEffect(() => {
    if (!slug || diarizeData) return;
    const bp = getBasePath();
    if (window.location.pathname !== bp + '/' + slug) {
      history.pushState({ slug }, '', bp + '/' + slug);
    }

    getTranscript(slug)
      .then((data) => {
        const t = data.transcript;
        if (t.source_url) setSourceURL(t.source_url);
        if (t.title) {
          setSourceTitle(t.title);
          document.title = t.title + ' — argraphments';
        }

        if (data.messages?.length > 0) {
          const dd = { speakers: data.speakers || {}, messages: data.messages };
          assignWordBasedTimestamps(dd.messages);
          setDiarizeData(dd);

          const names: Record<string, string> = {};
          const autoGen: Record<string, boolean> = {};
          const dbIds: Record<string, number> = {};
          for (const [sid, name] of Object.entries(data.speakers || {})) {
            names[sid] = name || sid.replace('_', ' ').replace(/\b\w/g, (c) => c.toUpperCase());
          }
          if (data.speaker_info) {
            for (const [sid, info] of Object.entries(data.speaker_info)) {
              autoGen[sid] = !!info.auto_generated;
              if (info.id) dbIds[sid] = info.id;
              if (info.name) names[sid] = info.name;
            }
          }
          setSpeakerNames(names);
          setSpeakerAutoGen(autoGen);
          setSpeakerDbIds(dbIds);

          setFullTranscript(
            data.messages.map((m) => `${names[m.speaker] || m.speaker}: ${m.text}`).join('\n')
          );
        }

        if (data.statements?.length > 0) {
          setAnalyzedStatements(data.statements);
          lastAnalyzedTranscript.current = data.messages
            ?.map((m) => `${data.speakers?.[m.speaker] || m.speaker}: ${m.text}`)
            .join('\n') || '';
        }

        setShowFinal(true);
      })
      .catch((e) => {
        console.error('Failed to load conversation:', e);
      });
  }, [slug]);

  const handleReanalyze = useCallback(() => {
    const transcript = buildTranscriptText();
    setAnalyzedStatements([]);
    lastAnalyzedTranscript.current = '';
    pendingAnalyze.current = true;
    analyzeAsync(transcript, true).finally(() => {
      pendingAnalyze.current = false;
    });
  }, [buildTranscriptText, setAnalyzedStatements, analyzeAsync, pendingAnalyze, lastAnalyzedTranscript]);

  const goHome = () => {
    resetSpeakers();
    setView('home');
    history.pushState(null, '', getBasePath() + '/');
    document.title = 'argraphments';
  };

  return (
    <div className="container">
      <AppHeader />
      <SessionHeader
        isRecording={isRecording}
        onStop={stopRecording}
        recordTime={recordTime}
      />
      {(() => {
        const m = sourceURL?.match(/(?:v=|youtu\.be\/)([a-zA-Z0-9_-]{11})/);
        return m ? <YouTubeEmbed videoId={m[1]} autoplay={isRecording} /> : null;
      })()}
      <div className="live-session">
        <TranscriptPanel
          diarizeData={diarizeData}
          highlightIdx={highlightIdx}
          pinnedIdx={pinnedIdx}
          onHover={onHover}
          onPin={onPin}
        />
        {analyzedStatements.length > 0 && (
          <ArgumentTree
            statements={analyzedStatements}
            diarizeData={diarizeData}
            highlightIdx={highlightIdx}
            pinnedIdx={pinnedIdx}
            onHover={onHover}
            onPin={onPin}
          />
        )}
        {showFinal && (
          <div className="analyze-form">
            <div className="action-row">
              <button className="btn" onClick={handleReanalyze}>Re-analyze</button>
              <button className="btn btn-secondary" onClick={goHome}>New</button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
