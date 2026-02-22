import React, { useEffect, useState } from 'react';
import { getBasePath, listTranscripts } from '../api';
import { useSession } from '../context/SessionContext';
import type { TranscriptListItem } from '../types';
import AppHeader from './AppHeader';

export default function ConversationsListPage() {
  const [convos, setConvos] = useState<TranscriptListItem[] | null>(null);
  const { setView, setSlug } = useSession();
  const bp = getBasePath();

  useEffect(() => {
    listTranscripts().then(setConvos).catch(() => setConvos([]));
  }, []);

  const loadConvo = (slug: string, e: React.MouseEvent) => {
    e.preventDefault();
    setSlug(slug);
    setView('session');
    history.pushState({ slug }, '', bp + '/convo/' + slug);
  };

  return (
    <div className="container">
      <AppHeader />
      <div className="list-page">
        <h2>Conversations</h2>
        {convos === null ? (
          <p className="text-dim">Loading…</p>
        ) : convos.length === 0 ? (
          <p className="text-dim">No conversations yet. Record or paste one!</p>
        ) : (
          <div className="list-items">
            {convos.map((t) => {
              const date = new Date(t.created_at).toLocaleDateString('en-US', {
                month: 'short', day: 'numeric', year: 'numeric',
              });
              const title = t.title ? t.title.substring(0, 80) : 'Untitled';
              return (
                <a
                  key={t.slug}
                  className="list-item"
                  href={bp + '/convo/' + t.slug}
                  onClick={(e) => loadConvo(t.slug, e)}
                >
                  <div>
                    <span className="list-item-name">{t.slug}</span>
                    <span className="list-item-title">{title}</span>
                  </div>
                  <span className="list-item-meta">{date}</span>
                </a>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
