import React from 'react';
import { formatMs } from '../utils/format';
import type { DiarizeMessage } from '../types';

interface Props {
  msg: DiarizeMessage;
  idx: string;
  speakerName: string;
  color: string;
  isHighlighted: boolean;
  isPinned: boolean;
  onHover: (idx: string | null) => void;
  onPin: (idx: string) => void;
}

export default React.memo(function ChatMessage({ msg, idx, speakerName, color, isHighlighted, isPinned, onHover, onPin }: Props) {
  return (
    <div
      className={`chat-msg${isHighlighted ? ' msg-highlight-self' : ''}${isPinned ? ' msg-pinned' : ''}`}
      data-speaker={msg.speaker}
      data-msg-idx={idx}
      style={{ '--speaker-bg': color } as React.CSSProperties}
      onMouseEnter={() => onHover(idx)}
    >
      {msg.start_ms != null && <span className="msg-time">{formatMs(msg.start_ms)}</span>}
      <span className="chat-speaker">{speakerName}</span>
      <span className="chat-text">{msg.text}</span>
      <button
        className={`btn-pin${isPinned ? ' pinned' : ''}`}
        onClick={(e) => { e.stopPropagation(); onPin(idx); }}
        title={isPinned ? 'Unpin' : 'Pin highlight'}
      >📌</button>
    </div>
  );
});
