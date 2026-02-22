import React, { useEffect, useState } from 'react';
import { getBasePath, getSpeaker } from '../api';
import { useSession } from '../context/SessionContext';
import type { SpeakerConversation } from '../types';
import { getConversationDisplayTitle } from '../utils/format';
import AppHeader from './AppHeader';

export default function SpeakerDetailPage() {
  const { speakerPageName, setView, setSlug } = useSession();
  const [convos, setConvos] = useState<SpeakerConversation[] | null>(null);
  const bp = getBasePath();

  useEffect(() => {
    if (!speakerPageName) return;
    getSpeaker(speakerPageName)
      .then((data) => setConvos(data.conversations || []))
      .catch(() => setConvos([]));
  }, [speakerPageName]);

  const loadConvo = (slug: string, e: React.MouseEvent) => {
    e.preventDefault();
    setSlug(slug);
    setView('session');
    history.pushState({ slug }, '', bp + '/convo/' + slug);
  };

  return (
    <div className="container">
      <AppHeader />
      <div className="speaker-page">
        <h2>{speakerPageName}</h2>
        {convos === null ? (
          <p className="text-dim">Loading…</p>
        ) : (
          <>
            <p className="text-dim">{convos.length} conversation{convos.length !== 1 ? 's' : ''}</p>
            {convos.length > 0 && (
              <div className="speaker-convos">
                {convos.map((c) => {
                  const date = new Date(c.created_at).toLocaleDateString('en-US', {
                    month: 'short', day: 'numeric', year: 'numeric',
                  });
                  const displayTitle = getConversationDisplayTitle(c.title, c.slug);
                  return (
                    <a
                      key={c.slug}
                      className="conversation-item"
                      href={bp + '/convo/' + c.slug}
                      onClick={(e) => loadConvo(c.slug, e)}
                    >
                      <div className="conversation-title">{displayTitle}</div>
                      <div className="conversation-meta">{date} · {c.claim_count} claims</div>
                    </a>
                  );
                })}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
