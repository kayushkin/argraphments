import React, { createContext, useContext, useState, useCallback, useRef } from 'react';
import type { DiarizeData, Statement } from '../types';
import * as api from '../api';
import { assignWordBasedTimestamps } from '../utils/timestamps';
import { useSpeakers } from './SpeakerContext';

interface SessionContextValue {
  slug: string | null;
  setSlug: (s: string | null) => void;
  isRecording: boolean;
  setIsRecording: (v: boolean) => void;
  diarizeData: DiarizeData | null;
  setDiarizeData: (d: DiarizeData | null) => void;
  fullTranscript: string;
  setFullTranscript: (t: string) => void;
  analyzedStatements: Statement[];
  setAnalyzedStatements: React.Dispatch<React.SetStateAction<Statement[]>>;
  lastAnalyzedTranscript: React.MutableRefObject<string>;
  sourceURL: string;
  setSourceURL: (u: string) => void;
  sourceTitle: string;
  setSourceTitle: (t: string) => void;
  pendingAnalyze: React.MutableRefObject<boolean>;
  pendingDiarize: React.MutableRefObject<boolean>;
  pendingTranscribe: React.MutableRefObject<boolean>;
  pendingYouTubeRecord: React.MutableRefObject<boolean>;
  diarizeAsync: (transcript: string) => Promise<void>;
  analyzeAsync: (transcript: string, forceFullReanalysis?: boolean) => Promise<void>;
  buildTranscriptText: () => string;
  createNewSession: () => Promise<string | null>;
  resetSession: () => void;
  view: 'home' | 'session' | 'speakers' | 'conversations' | 'speaker';
  setView: (v: 'home' | 'session' | 'speakers' | 'conversations' | 'speaker') => void;
  speakerPageName: string;
  setSpeakerPageName: (n: string) => void;
  showFinal: boolean;
  setShowFinal: (v: boolean) => void;
}

const SessionContext = createContext<SessionContextValue>(null!);
export const useSession = () => useContext(SessionContext);

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const [slug, setSlugState] = useState<string | null>(null);
  const [isRecording, setIsRecording] = useState(false);
  const [diarizeData, setDiarizeData] = useState<DiarizeData | null>(null);
  const [fullTranscript, setFullTranscript] = useState('');
  const [analyzedStatements, setAnalyzedStatements] = useState<Statement[]>([]);
  const lastAnalyzedTranscript = useRef('');
  const [sourceURL, setSourceURL] = useState('');
  const [sourceTitle, setSourceTitle] = useState('');
  const pendingAnalyze = useRef(false);
  const pendingDiarize = useRef(false);
  const pendingTranscribe = useRef(false);
  const pendingYouTubeRecord = useRef(false);
  const lastDiarizedText = useRef('');
  const diarizeCallCount = useRef(0);
  const [view, setView] = useState<'home' | 'session' | 'speakers' | 'conversations' | 'speaker'>('home');
  const [speakerPageName, setSpeakerPageName] = useState('');
  const [showFinal, setShowFinal] = useState(false);

  const { speakerNames, initSpeakersFromDiarize } = useSpeakers();

  const slugRef = useRef(slug);
  const setSlug = useCallback((s: string | null) => {
    slugRef.current = s;
    setSlugState(s);
  }, []);

  const speakerNamesRef = useRef(speakerNames);
  speakerNamesRef.current = speakerNames;

  const diarizeDataRef = useRef(diarizeData);
  diarizeDataRef.current = diarizeData;

  const statementsRef = useRef(analyzedStatements);
  statementsRef.current = analyzedStatements;

  const sourceURLRef = useRef(sourceURL);
  sourceURLRef.current = sourceURL;

  const buildTranscriptText = useCallback((): string => {
    const dd = diarizeDataRef.current;
    const names = speakerNamesRef.current;
    if (!dd) return fullTranscript;
    return dd.messages.map((m, i) => {
      const pos = m.position || i + 1;
      return `[${pos}] (${m.speaker}) ${names[m.speaker] || m.speaker}: ${m.text}`;
    }).join('\n');
  }, [fullTranscript]);

  const analyzeCallCount = useRef(0);
  const ANALYZE_FULL_REVIEW_EVERY = 5;
  const ANALYZE_CONTEXT_LINES = 8;

  const analyzeAsync = useCallback(
    async (transcript: string, forceFullReanalysis?: boolean) => {
      try {
        const lastTx = lastAnalyzedTranscript.current;
        const existing = statementsRef.current;

        if (!forceFullReanalysis && lastTx && existing.length > 0 && transcript.startsWith(lastTx.substring(0, 50))) {
          const newText = transcript.substring(lastTx.length).trim();
          if (!newText || newText.length < 20) return;

          analyzeCallCount.current++;

          // Build context from recent lines of already-analyzed transcript
          const allLines = lastTx.split('\n').filter((l) => l.trim());
          const contextLines = allLines.slice(-ANALYZE_CONTEXT_LINES);
          const contextText = contextLines.join('\n');

          const fullReview = analyzeCallCount.current % ANALYZE_FULL_REVIEW_EVERY === 0;

          // newText is already pre-numbered with [N] positions, no offset needed
          const data = await api.analyzeIncremental(newText, existing, 0, contextText, fullReview);

          lastAnalyzedTranscript.current = transcript;

          // Apply updates to existing statements
          if (data.updates?.length) {
            setAnalyzedStatements((prev) => {
              const applyUpdates = (stmts: Statement[]): Statement[] =>
                stmts.map((s) => {
                  const update = data.updates!.find((u) => u.msg_index === s.msg_index);
                  if (update) {
                    const updated = { ...s };
                    if (update.text) updated.text = update.text;
                    if (update.type) updated.type = update.type;
                    return updated;
                  }
                  if (s.children?.length) {
                    return { ...s, children: applyUpdates(s.children) };
                  }
                  return s;
                });

              const next = applyUpdates(prev);

              // Add new statements
              for (const ns of data.statements || []) {
                if (ns.parent_text) {
                  const parent = findByText(next, ns.parent_text);
                  if (parent) {
                    if (!parent.children) parent.children = [];
                    parent.children.push(ns);
                    continue;
                  }
                }
                next.push(ns);
              }
              return next;
            });
          } else {
            // Just add new statements
            const newStmts = data.statements || [];
            if (newStmts.length > 0) {
              setAnalyzedStatements((prev) => {
                const next = [...prev];
                for (const s of newStmts) {
                  if (s.parent_text) {
                    const parent = findByText(next, s.parent_text);
                    if (parent) {
                      if (!parent.children) parent.children = [];
                      parent.children.push(s);
                      continue;
                    }
                  }
                  next.push(s);
                }
                return next;
              });
            }
          }
        } else {
          // Full analysis
          const dd = diarizeDataRef.current;
          const names = speakerNamesRef.current;
          const data = await api.analyze(
            transcript,
            slugRef.current || undefined,
            sourceURLRef.current || undefined,
            Object.keys(names).length > 0 ? names : dd?.speakers,
            dd?.messages
          );

          lastAnalyzedTranscript.current = transcript;
          analyzeCallCount.current = 0;

          if (data.transcript_id) {
            setAnalyzedStatements(data.statements || []);
            if (data.title) setSourceTitle(data.title);
          } else {
            setAnalyzedStatements(data.statements || []);
          }
        }
      } catch (e) {
        console.warn('Analyze failed:', e);
      }
    },
    []
  );

  const applyDiarizeResult = useCallback(
    (data: DiarizeData) => {
      assignWordBasedTimestamps(data.messages);

      // Detect speaker reassignments by comparing old and new messages
      const oldData = diarizeDataRef.current;
      if (oldData && oldData.messages.length > 0) {
        const changedIndices = new Map<number, string>();
        const minLen = Math.min(oldData.messages.length, data.messages.length);
        for (let i = 0; i < minLen; i++) {
          if (oldData.messages[i].speaker !== data.messages[i].speaker) {
            changedIndices.set(i + 1, data.messages[i].speaker);
          }
        }
        if (changedIndices.size > 0) {
          const names = { ...speakerNamesRef.current, ...data.speakers };
          setAnalyzedStatements((prev) => {
            const updateStmts = (stmts: Statement[]): Statement[] =>
              stmts.map((s) => {
                const updated = { ...s };
                if (s.msg_index != null && changedIndices.has(s.msg_index)) {
                  const newSid = changedIndices.get(s.msg_index)!;
                  updated.speaker_id = newSid;
                  updated.speaker = names[newSid] || newSid;
                }
                if (s.children?.length) {
                  updated.children = updateStmts(s.children);
                }
                return updated;
              });
            return updateStmts(prev);
          });
        }
      }

      setDiarizeData(data);
      initSpeakersFromDiarize(data.speakers);
    },
    [initSpeakersFromDiarize]
  );

  const FULL_DIARIZE_EVERY = 5; // full re-diarize every N calls for drift correction
  const CONTEXT_LINES = 4; // lines of context to include with incremental chunk

  const diarizeAsync = useCallback(
    async (transcript: string) => {
      try {
        diarizeCallCount.current++;
        const lastText = lastDiarizedText.current;
        const oldData = diarizeDataRef.current;
        const isIncremental = lastText && transcript.startsWith(lastText.substring(0, 50))
          && oldData && oldData.messages.length > 0
          && (diarizeCallCount.current % FULL_DIARIZE_EVERY !== 0);

        if (isIncremental) {
          // Only diarize the new portion with some context
          const newText = transcript.substring(lastText.length).trim();
          if (!newText) return;

          // Build context from last few diarized messages
          const contextMsgs = oldData.messages.slice(-CONTEXT_LINES);
          const contextText = contextMsgs
            .map((m) => `${speakerNamesRef.current[m.speaker] || m.speaker}: ${m.text}`)
            .join('\n');
          const chunkToSend = contextText + '\n' + newText;

          const data = await api.diarize(chunkToSend);
          if ((data as any).error) return;

          // Merge: keep old messages, append only genuinely new ones
          // The response includes context messages + new ones; skip the context portion
          const newMsgs = data.messages.slice(contextMsgs.length);
          if (newMsgs.length === 0) return;

          // Merge speakers
          const mergedSpeakers = { ...oldData.speakers };
          for (const [id, name] of Object.entries(data.speakers)) {
            if (!mergedSpeakers[id] || (!mergedSpeakers[id] && name)) {
              mergedSpeakers[id] = name;
            }
          }

          // Map new message speaker IDs to match existing ones if needed
          // (Claude may use speaker_1/speaker_2 in the chunk, need to align)
          const merged: DiarizeData = {
            speakers: mergedSpeakers,
            messages: [...oldData.messages, ...newMsgs],
          };

          lastDiarizedText.current = transcript;
          applyDiarizeResult(merged);
        } else {
          // Full diarize
          const data = await api.diarize(transcript);
          if ((data as any).error) return;
          lastDiarizedText.current = transcript;
          applyDiarizeResult(data);
        }

        // Trigger analysis if enough new content
        const dd = diarizeDataRef.current;
        if (dd) {
          const currentTranscript = dd.messages
            .map((m, i) => {
              const pos = m.position || i + 1;
              return `[${pos}] (${m.speaker}) ${speakerNamesRef.current[m.speaker] || m.speaker}: ${m.text}`;
            })
            .join('\n');
          if (!pendingAnalyze.current && currentTranscript.length > lastAnalyzedTranscript.current.length + 50) {
            pendingAnalyze.current = true;
            analyzeAsync(currentTranscript).finally(() => {
              pendingAnalyze.current = false;
            });
          }
        }
      } catch (e) {
        console.warn('Diarize failed:', e);
      }
    },
    [applyDiarizeResult, analyzeAsync]
  );

  const createNewSession = useCallback(async (): Promise<string | null> => {
    try {
      const data = await api.createSession();
      setSlug(data.slug);
      return data.slug;
    } catch {
      return null;
    }
  }, [setSlug]);

  const resetSession = useCallback(() => {
    setSlug(null);
    setIsRecording(false);
    setDiarizeData(null);
    setFullTranscript('');
    setAnalyzedStatements([]);
    lastAnalyzedTranscript.current = '';
    setSourceURL('');
    setSourceTitle('');
    setShowFinal(false);
  }, [setSlug]);

  return (
    <SessionContext.Provider
      value={{
        slug,
        setSlug,
        isRecording,
        setIsRecording,
        diarizeData,
        setDiarizeData,
        fullTranscript,
        setFullTranscript,
        analyzedStatements,
        setAnalyzedStatements,
        lastAnalyzedTranscript,
        sourceURL,
        setSourceURL,
        sourceTitle,
        setSourceTitle,
        pendingAnalyze,
        pendingDiarize,
        pendingTranscribe,
        pendingYouTubeRecord,
        diarizeAsync,
        analyzeAsync,
        buildTranscriptText,
        createNewSession,
        resetSession,
        view,
        setView,
        speakerPageName,
        setSpeakerPageName,
        showFinal,
        setShowFinal,
      }}
    >
      {children}
    </SessionContext.Provider>
  );
}

function findByText(statements: Statement[], text: string): Statement | null {
  const needle = text.toLowerCase().trim();
  for (const s of statements) {
    if (s.text?.toLowerCase().trim() === needle) return s;
    if (s.children) {
      const found = findByText(s.children, text);
      if (found) return found;
    }
  }
  return null;
}
