import { useState, useCallback, useRef } from 'react';

export function useHighlight() {
  const [highlightIdx, setHighlightIdx] = useState<string | null>(null);
  const [pinnedIdx, setPinnedIdx] = useState<string | null>(null);
  const activeRef = useRef<string | null>(null);

  const onHover = useCallback((idx: string | null) => {
    if (idx) {
      activeRef.current = idx;
      setHighlightIdx(idx);
    } else {
      activeRef.current = null;
      // Fall back to pinned when hover ends
      setPinnedIdx((pinned) => {
        setHighlightIdx(pinned);
        return pinned;
      });
    }
  }, []);

  const onPin = useCallback((idx: string) => {
    setPinnedIdx((prev) => {
      const next = prev === idx ? null : idx;
      // If not currently hovering something else, update highlight
      if (!activeRef.current) setHighlightIdx(next);
      return next;
    });
  }, []);

  return { highlightIdx, pinnedIdx, onHover, onPin };
}
