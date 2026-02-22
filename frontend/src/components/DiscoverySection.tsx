import React, { useEffect, useState } from 'react';
import { getBasePath, listSpeakers, listTranscripts } from '../api';
import { useSession } from '../context/SessionContext';
import { useSpeakers } from '../context/SpeakerContext';
import type { SpeakerSummary, TranscriptListItem } from '../types';

export default function DiscoverySection() {
  const [speakers, setSpeakers] = useState<SpeakerSummary[]>([]);
  const [convos, setConvos] = useState<TranscriptListItem[]>([]);
  const { setView, setSlug, setSpeakerPageName } = useSession();
  const bp = getBasePath();

  useEffect(() => {
    listSpeakers().then(setSpeakers).catch(() => {});
    listTranscripts().then(setConvos).catch(() => {});
  }, []);

  if (!speakers.length && !convos.length) return null;

  const loadConvo = (slug: string, e: React.MouseEvent) => {
    e.preventDefault();
    setSlug(slug);
    setView('session');
    history.pushState({ slug }, '', bp + '/convo/' + slug);
  };

  const loadSpeaker = (name: string, e: React.MouseEvent) => {
    e.preventDefault();
    setSpeakerPageName(name);
    setView('speaker');
    history.pushState({ speaker: name }, '', bp + '/speaker/' + encodeURIComponent(name));
  };

  return (
    <div className="discovery" style={{ display: '' }}>
      {speakers.length > 0 && (
        <div className="discovery-col" id="discovery-speakers">
          <h3>Speakers</h3>
          <div className="speakers-list">
            {speakers.slice(0, 10).map((s) => (
              <a
                key={s.id}
                className="speaker-chip"
                href={bp + '/speaker/' + encodeURIComponent(s.name)}
                onClick={(e) => loadSpeaker(s.name, e)}
              >
                <span className="speaker-chip-name">{s.name}</span>
                <span className="speaker-chip-meta">
                  {s.conversation_count} convo{s.conversation_count !== 1 ? 's' : ''} · {s.claim_count} claims
                </span>
              </a>
            ))}
          </div>
        </div>
      )}
      {convos.length > 0 && (
        <div className="discovery-col" id="discovery-conversations">
          <h3>Conversations</h3>
          <div className="conversations-list">
            {convos.slice(0, 10).map((t) => {
              const date = new Date(t.created_at).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
              const title = t.title ? t.title.substring(0, 60) : 'Untitled';
              return (
                <a
                  key={t.slug}
                  className="conversation-item"
                  href={bp + '/convo/' + t.slug}
                  onClick={(e) => loadConvo(t.slug, e)}
                >
                  <div className="conversation-title">{t.slug} — {title}</div>
                  <div className="conversation-meta">{date}</div>
                </a>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}
