import React, { useRef } from 'react';
import { useSpeakers } from '../context/SpeakerContext';
import StatementNode from './StatementNode';
import type { Statement, DiarizeData } from '../types';

interface Props {
  statements: Statement[];
  diarizeData: DiarizeData | null;
  highlightIdx: string | null;
  pinnedIdx: string | null;
  onHover: (idx: string | null) => void;
  onPin: (idx: string) => void;
}

let globalIdCounter = 0;
function assignIds(stmts: Statement[]) {
  for (const s of stmts) {
    if (!s._id) s._id = 'stmt-' + (++globalIdCounter);
    if (s.children) assignIds(s.children);
  }
}

export default function ArgumentTree({ statements, diarizeData, highlightIdx, pinnedIdx, onHover, onPin }: Props) {
  const { resolveSpeaker, getSpeakerColor } = useSpeakers();
  const [collapsed, setCollapsed] = React.useState(false);

  assignIds(statements);

  return (
    <div className={`argument-section${collapsed ? ' collapsed' : ''}`} id="argument-section" style={{ display: '' }}>
      <div className="section-header">
        <span className="section-label">Argument Tree</span>
        <button className="btn-minimize" onClick={() => setCollapsed(!collapsed)}>
          {collapsed ? '+' : '−'}
        </button>
      </div>
      {!collapsed && (
        <div className="section-body">
          <div
            className={`argument-tree${highlightIdx ? ' has-highlight' : ''}`}
            onMouseLeave={() => onHover(null)}
          >
            {statements.map((s) => (
              <StatementNode
                key={s._id}
                statement={s}
                depth={0}
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
        </div>
      )}
    </div>
  );
}
