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

// What the server says when an API call needs a sign-in it does not have.
export interface SignInRefusal {
  status: number;
  error: string;
  sign_in_page_url: string;
  cookie_domain: string;
}

export const signInNeededEvent = 'argraphments-sign-in-needed';

export class SignInNeededError extends Error {
  constructor(public refusal: SignInRefusal) {
    super(refusal.error);
  }
}

// apiFetch is fetch for every /api/ call. A 401 or 403 tells the page to show
// the sign-in notice and rejects, so no caller treats the refusal as data.
async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  const resp = await apiFetch(path, init);
  if (resp.status === 401 || resp.status === 403) {
    const body = await resp.json();
    const refusal: SignInRefusal = { status: resp.status, ...body };
    window.dispatchEvent(new CustomEvent<SignInRefusal>(signInNeededEvent, { detail: refusal }));
    throw new SignInNeededError(refusal);
  }
  return resp;
}

export async function createSession(): Promise<{ slug: string; id: number }> {
  const resp = await apiFetch('/api/session/new', { method: 'POST' });
  return resp.json();
}

export async function transcribeAudio(form: FormData): Promise<{ text: string }> {
  const resp = await apiFetch('/api/transcribe', { method: 'POST', body: form });
  return resp.json();
}

export async function diarize(transcript: string, segments?: unknown[]): Promise<DiarizeData> {
  const body: Record<string, unknown> = { transcript };
  if (segments?.length) body.segments = segments;
  const resp = await apiFetch('/api/diarize', {
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
  const resp = await apiFetch('/api/analyze', {
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
  const resp = await apiFetch('/api/analyze-incremental', {
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
  const resp = await apiFetch('/api/transcripts');
  return resp.json();
}

export async function getTranscript(slug: string): Promise<TranscriptDetail> {
  const resp = await apiFetch('/api/transcripts/' + encodeURIComponent(slug));
  return resp.json();
}

export async function updateTranscriptSpeakers(
  slug: string,
  speakers: Record<string, string>,
  speakerAutoGen: Record<string, boolean>
): Promise<void> {
  await apiFetch('/api/transcripts/' + encodeURIComponent(slug) + '/speakers', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ speakers, speaker_auto_gen: speakerAutoGen }),
  });
}

export async function listSpeakers(): Promise<SpeakerSummary[]> {
  const resp = await apiFetch('/api/speakers');
  return resp.json();
}

export async function getSpeaker(name: string): Promise<SpeakerDetail> {
  const resp = await apiFetch('/api/speakers/' + encodeURIComponent(name));
  return resp.json();
}

export async function renameSpeakerAPI(oldName: string, newName: string): Promise<void> {
  await apiFetch('/api/speakers/' + encodeURIComponent(oldName), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name: newName }),
  });
}

export async function fetchSample(): Promise<SampleResponse> {
  const resp = await apiFetch('/api/sample', { method: 'POST' });
  if (!resp.ok) {
    const data = await resp.json();
    throw new Error(data.error || 'Failed');
  }
  return resp.json();
}

export async function importYouTubeTitleOnly(url: string): Promise<{ title?: string }> {
  const resp = await apiFetch('/api/import/youtube', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, title_only: true }),
  });
  return resp.json();
}
