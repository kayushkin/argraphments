import React from 'react';
import { getBasePath } from '../api';
import { useSession } from '../context/SessionContext';
import { useSpeakers } from '../context/SpeakerContext';

export default function AppHeader() {
  const { setView, resetSession, setSpeakerPageName } = useSession();
  const { resetSpeakers } = useSpeakers();
  const bp = getBasePath();

  const goHome = (e: React.MouseEvent) => {
    e.preventDefault();
    resetSession();
    resetSpeakers();
    setView('home');
    history.pushState(null, '', bp + '/');
    document.title = 'argraphments';
  };

  const goSpeakers = (e: React.MouseEvent) => {
    e.preventDefault();
    setView('speakers');
    history.pushState({ page: 'speakers' }, '', bp + '/speakers');
  };

  const goConversations = (e: React.MouseEvent) => {
    e.preventDefault();
    setView('conversations');
    history.pushState({ page: 'conversations' }, '', bp + '/conversations');
  };

  return (
    <header>
      <a href={bp + '/'} onClick={goHome} className="header-brand">
        argraphments
      </a>
      <nav className="header-nav">
        <a href={bp + '/speakers'} onClick={goSpeakers}>speakers</a>
        <a href={bp + '/conversations'} onClick={goConversations}>conversations</a>
      </nav>
      <a href="https://kayushkin.com" className="attribution">kayushkin.com</a>
    </header>
  );
}
