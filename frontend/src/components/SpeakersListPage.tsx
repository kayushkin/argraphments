import React, { useEffect, useState } from 'react';
import { getBasePath, listSpeakers } from '../api';
import { useSession } from '../context/SessionContext';
import type { SpeakerSummary } from '../types';
import AppHeader from './AppHeader';

export default function SpeakersListPage() {
  const [speakers, setSpeakers] = useState<SpeakerSummary[] | null>(null);
  const { setView, setSpeakerPageName } = useSession();
  const bp = getBasePath();

  useEffect(() => {
    listSpeakers().then(setSpeakers).catch(() => setSpeakers([]));
  }, []);

  const goSpeaker = (name: string, e: React.MouseEvent) => {
    e.preventDefault();
    setSpeakerPageName(name);
    setView('speaker');
    history.pushState({ speaker: name }, '', bp + '/speaker/' + encodeURIComponent(name));
  };

  return (
    <div className="container">
      <AppHeader />
      <div className="list-page">
        <h2>Speakers</h2>
        {speakers === null ? (
          <p className="text-dim">Loading…</p>
        ) : speakers.length === 0 ? (
          <p className="text-dim">No speakers yet. Start a conversation!</p>
        ) : (
          <div className="list-items">
            {speakers.map((s) => (
              <a
                key={s.id}
                className="list-item"
                href={bp + '/speaker/' + encodeURIComponent(s.name)}
                onClick={(e) => goSpeaker(s.name, e)}
              >
                <span className="list-item-name">{s.name}</span>
                <span className="list-item-meta">
                  {s.conversation_count} convo{s.conversation_count !== 1 ? 's' : ''} · {s.claim_count} claims
                </span>
              </a>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
