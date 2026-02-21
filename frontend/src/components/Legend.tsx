import React from 'react';
import { TYPE_EMOJIS } from '../types';

export default function Legend() {
  return (
    <div className="type-legend" id="header-legend">
      <span className="legend-trigger">🏷️ Legend</span>
      <div className="legend-dropdown">
        {Object.entries(TYPE_EMOJIS).map(([type, emoji]) => (
          <span key={type} className="legend-item">{emoji} {type}</span>
        ))}
      </div>
    </div>
  );
}
