import React, { useEffect, useState } from 'react';
import { signInNeededEvent, type SignInRefusal } from '../api';

// The sign-in cookie is set for the cookie domain and the names under it, so
// on any other host it is never sent and signing in there cannot help.
function hostGetsSignInCookie(cookieDomain: string): boolean {
  const host = window.location.hostname;
  return host === cookieDomain || host.endsWith('.' + cookieDomain);
}

export default function SignInNotice() {
  const [refusal, setRefusal] = useState<SignInRefusal | null>(null);

  useEffect(() => {
    const handler = (event: Event) => setRefusal((event as CustomEvent<SignInRefusal>).detail);
    window.addEventListener(signInNeededEvent, handler);
    return () => window.removeEventListener(signInNeededEvent, handler);
  }, []);

  if (!refusal) return null;

  if (!hostGetsSignInCookie(refusal.cookie_domain)) {
    const appAddress = 'https://' + refusal.cookie_domain + '/argraphments';
    return (
      <div className="sign-in-notice" role="alert">
        argraphments needs a sign-in, which works only at{' '}
        <a href={appAddress}>{appAddress}</a>. Use it there.
      </div>
    );
  }
  if (refusal.status === 403) {
    return (
      <div className="sign-in-notice" role="alert">
        You are signed in, but only an administrator may use argraphments.
      </div>
    );
  }
  return (
    <div className="sign-in-notice" role="alert">
      argraphments needs a sign-in. <a href={refusal.sign_in_page_url}>Sign in</a>, then reload this page.
    </div>
  );
}
