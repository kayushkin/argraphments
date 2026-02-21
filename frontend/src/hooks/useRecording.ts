import { useRef, useCallback, useState, useEffect } from 'react';
import * as api from '../api';
import { useSession } from '../context/SessionContext';

const CHUNK_INTERVAL_MS = 10000;
const OVERLAP_MS = 5000; // 5s overlap for continuity at boundaries

interface TimedChunk {
  blob: Blob;
  timestampMs: number; // ms since recording started
}

export function useRecording() {
  const {
    setIsRecording,
    setFullTranscript,
    fullTranscript,
    diarizeAsync,
    pendingTranscribe,
    pendingDiarize,
    createNewSession,
    setSourceURL,
    setSourceTitle,
    setShowFinal,
    buildTranscriptText,
    analyzeAsync,
    pendingAnalyze,
    lastAnalyzedTranscript,
    setAnalyzedStatements,
  } = useSession();

  const mediaRecorderRef = useRef<MediaRecorder | null>(null);
  const timedChunksRef = useRef<TimedChunk[]>([]);
  const chunkIntervalRef = useRef<number | null>(null);
  const recordStartRef = useRef<number>(0);
  const [recordTime, setRecordTime] = useState('00:00');
  const recordTimerRef = useRef<number | null>(null);
  const fullTranscriptRef = useRef(fullTranscript);
  fullTranscriptRef.current = fullTranscript;

  // Locked transcript: text from audio that's been finalized (won't be re-sent)
  const lockedTextRef = useRef('');
  const lockedUpToMsRef = useRef(0); // audio ms up to which text is locked

  const updateTime = useCallback(() => {
    recordTimerRef.current = window.setInterval(() => {
      const elapsed = Math.floor((Date.now() - recordStartRef.current) / 1000);
      const m = String(Math.floor(elapsed / 60)).padStart(2, '0');
      const s = String(elapsed % 60).padStart(2, '0');
      setRecordTime(`${m}:${s}`);
    }, 1000);
  }, []);

  const processChunk = useCallback(async () => {
    if (pendingTranscribe.current || timedChunksRef.current.length === 0) return;
    pendingTranscribe.current = true;

    try {
      const allChunks = timedChunksRef.current;
      const lockedMs = lockedUpToMsRef.current;

      // Send all audio chunks — webm requires contiguous stream from start
      const blob = new Blob(allChunks.map((c) => c.blob), { type: 'audio/webm' });
      console.log(`[Recording] Sending ${allChunks.length} chunks, ${(blob.size / 1024).toFixed(0)}KB`);
      const form = new FormData();
      form.append('audio', blob, 'chunk.webm');

      const data = await api.transcribeAudio(form);
      const fullText = (data.text || '').trim();
      if (!fullText) return;
      if (fullText && fullText !== fullTranscriptRef.current) {
        setFullTranscript(fullText);
        fullTranscriptRef.current = fullText;

        if (!pendingDiarize.current) {
          pendingDiarize.current = true;
          diarizeAsync(fullText).finally(() => {
            pendingDiarize.current = false;
          });
        }
      }

      // Lock the current text — next call will still send all audio
      // (webm requires contiguous stream) but diarize/analyze only process new text
      lockedTextRef.current = fullText;
      lockedUpToMsRef.current = allChunks[allChunks.length - 1].timestampMs;
    } catch (e) {
      console.warn('Chunk failed:', e);
    } finally {
      pendingTranscribe.current = false;
    }
  }, [setFullTranscript, diarizeAsync, pendingTranscribe, pendingDiarize]);

  const initRecorder = useCallback(
    (stream: MediaStream) => {
      timedChunksRef.current = [];
      lockedTextRef.current = '';
      lockedUpToMsRef.current = 0;

      const mr = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' });
      mr.ondataavailable = (e) => {
        if (e.data.size > 0) {
          timedChunksRef.current.push({
            blob: e.data,
            timestampMs: Date.now() - recordStartRef.current,
          });
        }
      };
      mr.onstop = () => stream.getTracks().forEach((t) => t.stop());
      mr.start(1000);
      mediaRecorderRef.current = mr;
      recordStartRef.current = Date.now();
      updateTime();
      chunkIntervalRef.current = window.setInterval(processChunk, CHUNK_INTERVAL_MS);
      setIsRecording(true);
    },
    [updateTime, processChunk, setIsRecording]
  );

  const startRecording = useCallback(
    async (mode: 'mic' | 'tab'): Promise<boolean> => {
      try {
        let stream: MediaStream;
        if (mode === 'tab') {
          stream = await navigator.mediaDevices.getDisplayMedia({
            video: true,
            audio: true,
            preferCurrentTab: true,
            selfBrowserSurface: 'include',
          } as any);
          stream.getVideoTracks().forEach((t) => t.stop());
          if (stream.getAudioTracks().length === 0) return false;
        } else {
          stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        }
        initRecorder(stream);
        return true;
      } catch {
        return false;
      }
    },
    [initRecorder]
  );

  const stopRecording = useCallback(async () => {
    setIsRecording(false);
    if (chunkIntervalRef.current) clearInterval(chunkIntervalRef.current);
    if (recordTimerRef.current) clearInterval(recordTimerRef.current);

    const mr = mediaRecorderRef.current;
    if (mr) {
      mr.stop();
      mediaRecorderRef.current = null;
    }

    // Final transcription
    const allChunks = timedChunksRef.current;
    if (allChunks.length > 0) {
      const blob = new Blob(allChunks.map((c) => c.blob), { type: 'audio/webm' });
      const form = new FormData();
      form.append('audio', blob, 'recording.webm');
      try {
        const data = await api.transcribeAudio(form);
        const fullText = (data.text || '').trim() || fullTranscriptRef.current;
        setFullTranscript(fullText);
        fullTranscriptRef.current = fullText;
        await diarizeAsync(fullText);
      } catch {}
    }

    setShowFinal(true);

    setTimeout(() => {
      const transcript = buildTranscriptText();
      if (transcript !== lastAnalyzedTranscript.current) {
        pendingAnalyze.current = true;
        analyzeAsync(transcript).finally(() => {
          pendingAnalyze.current = false;
        });
      }
    }, 100);
  }, [
    setIsRecording,
    setFullTranscript,
    diarizeAsync,
    setShowFinal,
    buildTranscriptText,
    analyzeAsync,
    pendingAnalyze,
    lastAnalyzedTranscript,
  ]);

  const startYouTubeRecording = useCallback(
    async (videoId: string, url: string): Promise<boolean> => {
      setSourceURL(url);
      try {
        const stream = await navigator.mediaDevices.getDisplayMedia({
          video: true,
          audio: true,
          preferCurrentTab: true,
          selfBrowserSurface: 'include',
        } as any);
        stream.getVideoTracks().forEach((t) => t.stop());
        if (stream.getAudioTracks().length === 0) return false;

        initRecorder(stream);

        api.importYouTubeTitleOnly(url).then((data) => {
          if (data.title) setSourceTitle(data.title);
        }).catch(() => {});

        return true;
      } catch {
        return false;
      }
    },
    [initRecorder, setSourceURL, setSourceTitle]
  );

  useEffect(() => {
    return () => {
      if (chunkIntervalRef.current) clearInterval(chunkIntervalRef.current);
      if (recordTimerRef.current) clearInterval(recordTimerRef.current);
    };
  }, []);

  return {
    startRecording,
    stopRecording,
    startYouTubeRecording,
    recordTime,
  };
}
