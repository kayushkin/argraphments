import React, { useRef, useEffect } from 'react';
import { useSpeakers } from '../context/SpeakerContext';
import ChatMessage from './ChatMessage';
import type { DiarizeData } from '../types';

interface Props {
  diarizeData: DiarizeData | null;
  highlightIdx: string | null;
  pinnedIdx: string | null;
  onHover: (idx: string | null) => void;
  onPin: (idx: string) => void;
}

export default function TranscriptPanel({ diarizeData, highlightIdx, pinnedIdx, onHover, onPin }: Props) {
  const { speakerNames, getSpeakerColor } = useSpeakers();
  const containerRef = useRef<HTMLDivElement>(null);
  const [collapsed, setCollapsed] = React.useState(false);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [diarizeData]);

  return (
    <div className={`diarized-view${collapsed ? ' collapsed' : ''}`} id="diarized-view">
      <div className="section-header">
        <span className="section-label">Transcript</span>
        <button className="btn-minimize" onClick={() => setCollapsed(!collapsed)}>
          {collapsed ? '+' : '−'}
        </button>
      </div>
      {!collapsed && (
        <div className="section-body">
          <div
            className={`chat-messages${highlightIdx ? ' has-highlight' : ''}`}
            ref={containerRef}
            onMouseLeave={() => onHover(null)}
          >
            {diarizeData?.messages.map((msg, i) => {
              const idx = String(msg.position || i + 1);
              return (
                <ChatMessage
                  key={idx}
                  msg={msg}
                  idx={idx}
                  speakerName={speakerNames[msg.speaker] || msg.speaker}
                  color={getSpeakerColor(msg.speaker)}
                  isHighlighted={highlightIdx === idx}
                  isPinned={pinnedIdx === idx}
                  onHover={onHover}
                  onPin={onPin}
                />
              );
            })}
            {!diarizeData && (
              <div className="chat-msg">
                <span className="chat-text interim">Listening...</span>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
