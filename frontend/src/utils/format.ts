export function formatMs(ms: number): string {
  const totalSecs = Math.floor(ms / 1000);
  const h = Math.floor(totalSecs / 3600);
  const m = Math.floor((totalSecs % 3600) / 60);
  const s = totalSecs % 60;
  if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
  return `${m}:${String(s).padStart(2, '0')}`;
}

export function escapeHtml(text: string): string {
  const div = document.createElement('div');
  div.textContent = text;
  return div.innerHTML;
}

/**
 * Get a display title for a conversation.
 * Priority: 1. title field, 2. speaker names, 3. slug
 */
export function getConversationDisplayTitle(
  title: string | undefined | null,
  slug: string,
  speakers?: Record<string, string> | string[]
): string {
  // If we have a title, use it
  if (title && title.trim()) {
    return title.trim();
  }

  // If we have speaker names, generate "Speaker1 vs Speaker2"
  if (speakers) {
    const names = Array.isArray(speakers)
      ? speakers.filter(n => n && n.trim())
      : Object.values(speakers).filter(n => n && n.trim());
    
    if (names.length > 0) {
      // Filter out generic "Speaker N" patterns
      const meaningfulNames = names.filter(n => 
        !/^speaker[_ ]\d+$/i.test(n)
      );
      
      if (meaningfulNames.length > 0) {
        if (meaningfulNames.length === 1) {
          return meaningfulNames[0];
        }
        return meaningfulNames.join(' vs ');
      }
    }
  }

  // Fall back to slug
  return slug;
}
