import type {
  DiarizeData,
  TranscriptListItem,
  SpeakerSummary,
  TranscriptDetail,
  AnalyzeResponse,
  SampleResponse,
  Statement,
  SpeakerDetail,
  DiarizeMessage,
} from './types';

export function getBasePath(): string {
  return window.location.pathname.startsWith('/argraphments') ? '/argraphments' : '';
}

const bp = () => getBasePath();

export async function createSession(): Promise<{ slug: string; id: number }> {
  const resp = await fetch(bp() + '/api/session/new', { method: 'POST' });
  return resp.json();
}

export async function transcribeAudio(form: FormData): Promise<{ text: string }> {
  const resp = await fetch(bp() + '/api/transcribe', { method: 'POST', body: form });
  return resp.json();
}

export async function diarize(transcript: string, segments?: unknown[]): Promise<DiarizeData> {
  const body: Record<string, unknown> = { transcript };
  if (segments?.length) body.segments = segments;
  const resp = await fetch(bp() + '/api/diarize', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return resp.json();
}

export async function analyze(
  transcript: string,
  slug?: string,
  sourceURL?: string,
  speakers?: Record<string, string>,
  messages?: DiarizeMessage[],
  speakerAutoGen?: Record<string, boolean>
): Promise<AnalyzeResponse> {
  const body: Record<string, unknown> = { transcript };
  if (slug) body.slug = slug;
  if (sourceURL) body.source_url = sourceURL;
  if (speakers) body.speakers = speakers;
  if (messages) body.messages = messages;
  if (speakerAutoGen) body.speaker_auto_gen = speakerAutoGen;
  const resp = await fetch(bp() + '/api/analyze', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return resp.json();
}

export interface StatementUpdate {
  msg_index: number;
  text?: string;
  type?: string;
  parent_text?: string;
}

export async function analyzeIncremental(
  newText: string,
  existing: Statement[],
  msgOffset: number,
  contextText?: string,
  fullReview?: boolean
): Promise<{ statements: Statement[]; updates?: StatementUpdate[] }> {
  const resp = await fetch(bp() + '/api/analyze-incremental', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      new_text: newText,
      context_text: contextText || '',
      existing,
      msg_offset: msgOffset,
      full_review: !!fullReview,
    }),
  });
  return resp.json();
}

export async function listTranscripts(): Promise<TranscriptListItem[]> {
  const resp = await fetch(bp() + '/api/transcripts');
  return resp.json();
}

export async function getTranscript(slug: string): Promise<TranscriptDetail> {
  const resp = await fetch(bp() + '/api/transcripts/' + encodeURIComponent(slug));
  return resp.json();
}

export async function updateTranscriptSpeakers(
  slug: string,
  speakers: Record<string, string>,
  speakerAutoGen: Record<string, boolean>
): Promise<void> {
  await fetch(bp() + '/api/transcripts/' + encodeURIComponent(slug) + '/speakers', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ speakers, speaker_auto_gen: speakerAutoGen }),
  });
}

export async function listSpeakers(): Promise<SpeakerSummary[]> {
  const resp = await fetch(bp() + '/api/speakers');
  return resp.json();
}

export async function getSpeaker(name: string): Promise<SpeakerDetail> {
  const resp = await fetch(bp() + '/api/speakers/' + encodeURIComponent(name));
  return resp.json();
}

export async function renameSpeakerAPI(oldName: string, newName: string): Promise<void> {
  await fetch(bp() + '/api/speakers/' + encodeURIComponent(oldName), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: newName }),
  });
}

export async function fetchSample(): Promise<SampleResponse> {
  const resp = await fetch(bp() + '/api/sample', { method: 'POST' });
  if (!resp.ok) {
    const data = await resp.json();
    throw new Error(data.error || 'Failed');
  }
  return resp.json();
}

export async function importYouTubeTitleOnly(url: string): Promise<{ title?: string }> {
  const resp = await fetch(bp() + '/api/import/youtube', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, title_only: true }),
  });
  return resp.json();
}
