import React, { useEffect } from 'react';
import { SpeakerProvider } from '../context/SpeakerContext';
import { SessionProvider, useSession } from '../context/SessionContext';
import { getBasePath } from '../api';
import HomePage from './HomePage';
import SessionPage from './SessionPage';
import SpeakersListPage from './SpeakersListPage';
import ConversationsListPage from './ConversationsListPage';
import SpeakerDetailPage from './SpeakerDetailPage';

function Router() {
  const { view, setView, setSlug, setSpeakerPageName } = useSession();

  useEffect(() => {
    const bp = getBasePath();
    const path = window.location.pathname.replace(bp, '').replace(/^\/+/, '');

    if (path === 'speakers') {
      setView('speakers');
    } else if (path === 'conversations') {
      setView('conversations');
    } else if (path.startsWith('speaker/')) {
      setSpeakerPageName(decodeURIComponent(path.substring(8)));
      setView('speaker');
    } else if (path.startsWith('convo/')) {
      const slug = path.substring(6);
      setSlug(slug);
      setView('session');
    } else if (path && !path.includes('/') && !path.includes('.')) {
      // Fallback: old-style bare slug URLs (for backward compatibility)
      setSlug(path);
      setView('session');
    }
  }, []);

  useEffect(() => {
    const handler = (e: PopStateEvent) => {
      if (e.state?.slug) {
        setSlug(e.state.slug);
        setView('session');
      } else if (e.state?.speaker) {
        setSpeakerPageName(e.state.speaker);
        setView('speaker');
      } else if (e.state?.page === 'speakers') {
        setView('speakers');
      } else if (e.state?.page === 'conversations') {
        setView('conversations');
      } else {
        setView('home');
      }
    };
    window.addEventListener('popstate', handler);
    return () => window.removeEventListener('popstate', handler);
  }, [setView, setSlug, setSpeakerPageName]);

  if (view === 'speakers') return <SpeakersListPage />;
  if (view === 'conversations') return <ConversationsListPage />;
  if (view === 'speaker') return <SpeakerDetailPage />;
  if (view === 'session') return <SessionPage />;
  return <HomePage />;
}

export default function App() {
  return (
    <SpeakerProvider>
      <SessionProvider>
        <Router />
      </SessionProvider>
    </SpeakerProvider>
  );
}
