import React from 'react';
import { formatMs } from '../utils/format';
import { TYPE_EMOJIS } from '../types';
import type { Statement, DiarizeData } from '../types';

interface Props {
  statement: Statement;
  depth: number;
  diarizeData: DiarizeData | null;
  highlightIdx: string | null;
  pinnedIdx: string | null;
  onHover: (idx: string | null) => void;
  onPin: (idx: string) => void;
  resolveSpeaker: (raw: string) => string;
  getSpeakerColor: (speaker: string) => string;
}

function countDescendants(s: Statement): number {
  let count = (s.children || []).length;
  for (const c of s.children || []) count += countDescendants(c);
  return count;
}

function getMsgInfo(s: Statement, diarizeData: DiarizeData | null) {
  const idx = s.msg_index;
  if (idx == null || !diarizeData?.messages) return { idx: '', startMs: null as number | null };
  const msg = diarizeData.messages.find((m) => m.position === idx) || diarizeData.messages[idx - 1];
  return { idx: String(idx), startMs: msg?.start_ms ?? null };
}

export default function StatementNode({
  statement: s,
  depth,
  diarizeData,
  highlightIdx,
  pinnedIdx,
  onHover,
  onPin,
  resolveSpeaker,
  getSpeakerColor,
}: Props) {
  const typeClass = s.type || 'claim';
  const emoji = TYPE_EMOJIS[typeClass] || '💬';
  // Always resolve speaker from diarize data via msg_index (canonical source of truth)
  const speakerKey = (() => {
    if (s.msg_index != null && diarizeData?.messages) {
      const msg = diarizeData.messages.find((m) => m.position === s.msg_index) || diarizeData.messages[s.msg_index - 1];
      if (msg) return msg.speaker;
    }
    return s.speaker_id || s.speaker;
  })();
  const speaker = resolveSpeaker(speakerKey);
  const color = getSpeakerColor(speakerKey);
  const { idx, startMs } = getMsgInfo(s, diarizeData);
  const hasChildren = (s.children?.length || 0) > 0;
  const isHighlighted = idx === highlightIdx && idx !== '';
  const isPinned = idx === pinnedIdx && idx !== '';

  const flaggedClass =
    (s.fact_check ? ` flagged flagged-${s.fact_check.verdict}` : '') +
    (s.fallacy ? ' has-fallacy' : '');

  const className = `statement depth-${depth} type-${typeClass}${flaggedClass}${isHighlighted ? ' msg-highlight-self' : ''}${isPinned ? ' msg-pinned' : ''}`;

  const tsEl = startMs != null ? <span className="msg-time">{formatMs(startMs)}</span> : null;

  const meta = (
    <span className="stmt-meta">
      <span className="type-badge">{emoji}</span>
      {tsEl}
      <span className="speaker" style={{ color }}>{speaker}</span>
      {hasChildren && <span className="child-count">{countDescendants(s)}</span>}
      {idx && <button
        className={`btn-pin${isPinned ? ' pinned' : ''}`}
        onClick={(e) => { e.stopPropagation(); onPin(idx); }}
        title={isPinned ? 'Unpin' : 'Pin highlight'}
      >📌</button>}
    </span>
  );

  const body = (
    <>
      {s.text}
      {s.fact_check && <FactCheckBadge fc={s.fact_check} />}
      {s.fallacy && <FallacyBadge f={s.fallacy} />}
    </>
  );

  return (
    <div
      className={className}
      style={{ '--speaker-bg': color } as React.CSSProperties}
      data-msg-idx={idx || undefined}
    >
      {meta}
      {hasChildren ? (
        <details open>
          <summary onMouseEnter={() => idx && onHover(idx)}>
            {body}
          </summary>
          <div className="children">
            {s.children!.map((child) => (
              <StatementNode
                key={child._id || child.text}
                statement={child}
                depth={depth + 1}
                diarizeData={diarizeData}
                highlightIdx={highlightIdx}
                pinnedIdx={pinnedIdx}
                onHover={onHover}
                onPin={onPin}
                resolveSpeaker={resolveSpeaker}
                getSpeakerColor={getSpeakerColor}
              />
            ))}
          </div>
        </details>
      ) : (
        <div className="leaf" onMouseEnter={() => idx && onHover(idx)}>{body}</div>
      )}
    </div>
  );
}

function FactCheckBadge({ fc }: { fc: NonNullable<Statement['fact_check']> }) {
  const searchURL = 'https://www.google.com/search?q=' + encodeURIComponent(fc.search_query || '');
  return (
    <div className={`fact-check verdict-${fc.verdict}`}>
      <span className="fact-verdict">⚠ {fc.verdict}</span>
      <span className="fact-correction">{fc.correction}</span>
      <a href={searchURL} target="_blank" rel="noopener" className="fact-source">verify ↗</a>
    </div>
  );
}

function FallacyBadge({ f }: { f: NonNullable<Statement['fallacy']> }) {
  const searchURL = 'https://www.google.com/search?q=' + encodeURIComponent(f.name + ' logical fallacy');
  return (
    <div className="fallacy-flag">
      <span className="fallacy-name">🧠 {f.name}</span>
      <span className="fallacy-explanation">{f.explanation}</span>
      <a href={searchURL} target="_blank" rel="noopener" className="fact-source">learn more ↗</a>
    </div>
  );
}
