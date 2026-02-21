import type { DiarizeMessage } from '../types';

export function assignWordBasedTimestamps(messages: DiarizeMessage[]): void {
  if (!messages || !messages.length) return;
  if (messages.some((m) => m.start_ms != null)) return;
  let runningMs = 0;
  for (const msg of messages) {
    msg.start_ms = runningMs;
    const words = (msg.text || '').split(/\s+/).filter((w) => w).length;
    const durationMs = Math.max(2000, Math.round((words / 2.5) * 1000));
    msg.end_ms = runningMs + durationMs;
    runningMs += durationMs + 500;
  }
}
