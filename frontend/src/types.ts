export interface FactCheck {
  verdict: string;
  correction: string;
  search_query: string;
}

export interface Fallacy {
  name: string;
  explanation: string;
}

export interface Statement {
  speaker: string;
  speaker_id?: string;
  text: string;
  type: string;
  msg_index?: number;
  children?: Statement[];
  fact_check?: FactCheck;
  fallacy?: Fallacy;
  parent_text?: string;
  _id?: string;
}

export interface DiarizeMessage {
  speaker: string;
  text: string;
  position?: number;
  start_ms?: number;
  end_ms?: number;
}

export interface DiarizeData {
  speakers: Record<string, string>;
  messages: DiarizeMessage[];
}

export interface TranscriptListItem {
  id: number;
  slug: string;
  title: string;
  created_at: string;
}

export interface SpeakerSummary {
  id: number;
  name: string;
  conversation_count: number;
  claim_count: number;
}

export interface SpeakerInfo {
  name: string;
  auto_generated: boolean;
  id?: number;
}

export interface TranscriptDetail {
  transcript: {
    id: number;
    slug: string;
    title: string;
    source_url: string;
    created_at: string;
  };
  speakers: Record<string, string>;
  speaker_info: Record<string, SpeakerInfo>;
  messages: DiarizeMessage[];
  statements: Statement[];
}

export interface AnalyzeResponse {
  statements: Statement[];
  transcript_id: number;
  slug: string;
  title: string;
}

export interface SampleResponse {
  speakers: Record<string, string>;
  messages: DiarizeMessage[];
  text: string;
  title: string;
  url: string;
}

export interface SpeakerConversation {
  slug: string;
  title: string;
  created_at: string;
  claim_count: number;
}

export interface SpeakerDetail {
  name: string;
  conversations: SpeakerConversation[];
}

export const TYPE_EMOJIS: Record<string, string> = {
  claim: '💬',
  response: '↩️',
  question: '❓',
  agreement: '✅',
  rebuttal: '⚔️',
  tangent: '🌀',
  clarification: '🔍',
  evidence: '📎',
};
