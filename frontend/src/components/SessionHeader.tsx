import React from 'react';
import { useSession } from '../context/SessionContext';
import { useSpeakers } from '../context/SpeakerContext';
import { getBasePath } from '../api';
import { formatMs } from '../utils/format';
import Legend from './Legend';
import type { DiarizeMessage } from '../types';

interface Props {
  isRecording: boolean;
  onStop: () => void;
  recordTime: string;
}

export default function SessionHeader({ isRecording, onStop, recordTime }: Props) {
  const { slug, diarizeData, sourceTitle, sourceURL, analyzedStatements } = useSession();
  const { speakerNames, speakerAutoGen, speakerDbIds, renameSpeaker, getSpeakerColor } = useSpeakers();
  const bp = getBasePath();

  const hasNames = Object.keys(speakerNames).length > 0;

  // Compute speaker stats
  const speakerWords: Record<string, number> = {};
  const speakerMsgCount: Record<string, number> = {};
  const speakerTimeMs: Record<string, number> = {};

  if (diarizeData?.messages) {
    const msgs = diarizeData.messages;
    for (let i = 0; i < msgs.length; i++) {
      const msg = msgs[i];
      const wc = (msg.text || '').split(/\s+/).filter((w) => w).length;
      speakerWords[msg.speaker] = (speakerWords[msg.speaker] || 0) + wc;
      speakerMsgCount[msg.speaker] = (speakerMsgCount[msg.speaker] || 0) + 1;
      if (msg.start_ms != null) {
        const endMs = msg.end_ms ?? (msgs[i + 1]?.start_ms ?? msg.start_ms + 5000);
        speakerTimeMs[msg.speaker] = (speakerTimeMs[msg.speaker] || 0) + (endMs - msg.start_ms);
      }
    }
  }

  const isYT = /youtube\.com\/watch|youtu\.be\//.test(sourceURL);
  const videoId = sourceURL.match(/(?:v=|youtu\.be\/)([a-zA-Z0-9_-]{11})/)?.[1] || '';

  return (
    <div className="recording-bar" style={{ display: hasNames || isRecording ? '' : 'none' }}>
      <div className="recording-bar-top">
        {isRecording && (
          <>
            <span className="record-dot recording"></span>
            <span className="record-time">{recordTime}</span>
          </>
        )}

        {hasNames && (
          <div className="speaker-names">
            <div className="speaker-list">
              {Object.keys(speakerNames).map((id) => {
                const color = getSpeakerColor(id);
                const isAnon = speakerAutoGen[id];
                const name = speakerNames[id];
                const words = speakerWords[id] || 0;
                const msgs = speakerMsgCount[id] || 0;
                const timeMs = speakerTimeMs[id] || 0;

                return (
                  <div key={id} className="speaker-input" style={{ '--speaker-color': color } as React.CSSProperties}>
                    <span className="speaker-dot" style={{ background: color }}></span>
                    {isAnon && <span className="speaker-anon-tag" title="Anonymous — click name to rename">anon</span>}
                    {!isAnon && speakerDbIds[id] && (
                      <a className="speaker-link" href={bp + '/speaker/' + encodeURIComponent(name)} title="View speaker page">↗</a>
                    )}
                    <input
                      type="text"
                      defaultValue={name}
                      className={isAnon ? 'anon-name' : ''}
                      onBlur={(e) => {
                        if (e.target.value !== name) renameSpeaker(id, e.target.value, slug || undefined);
                      }}
                    />
                    <span className="speaker-stats">{words}w · {msgs} msgs · {formatMs(timeMs)}</span>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {analyzedStatements.length > 0 && <Legend />}

        {sourceTitle && (
          <div className="conversation-title-bar">
            {isYT && videoId ? (
              <span className="yt-title-group">
                <svg className="yt-icon" viewBox="0 0 28 20" xmlns="http://www.w3.org/2000/svg">
                  <path d="M27.4 3.1s-.3-1.8-1.1-2.6C25.2-.1 23.9-.1 23.3-.2 19.4-.4 14-.4 14-.4s-5.4 0-9.3.2c-.6.1-1.9.1-3 1.5C.9 2.3.6 4.1.6 4.1S.3 6.2.3 8.4v2c0 2.2.3 4.3.3 4.3s.3 1.8 1.1 2.6c1.1 1.1 2.4 1.1 3 1.2 2.2.2 9.3.3 9.3.3s5.4 0 9.3-.2c.6-.1 1.9-.1 3-1.5.8-.8 1.1-2.6 1.1-2.6s.3-2.2.3-4.3v-2c0-2.2-.3-4.3-.3-4.3z" fill="#FF0000"/>
                  <path d="M11.2 13.2V5.6l7.8 3.8-7.8 3.8z" fill="#FFF"/>
                </svg>
                <span>{sourceTitle}</span>
              </span>
            ) : (
              <span>{sourceTitle}</span>
            )}
          </div>
        )}

        {isRecording && (
          <button className="btn btn-stop" onClick={onStop}>Stop</button>
        )}
      </div>
    </div>
  );
}
