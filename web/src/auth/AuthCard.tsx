// Frame for the sign-in and sign-up pages, built from the legacy passphrase lock card (`.lockcard`)
// so they look like the rest of the app.

import { useEffect, type ReactNode, type SyntheticEvent } from 'react';

import { useAppearance } from '../shell/effects';
import { Logo } from '../ui/Logo';
import { Toaster } from '../ui/Toast';

type Props = { title: string; lead: ReactNode; onSubmit?: () => void; children: ReactNode };

export function AuthCard({ title, lead, onSubmit, children }: Props) {
  useAppearance();
  useEffect(() => {
    document.title = `${title} · Finuance`;
    return () => {
      document.title = 'Finuance';
    };
  }, [title]);
  const submit = (e: SyntheticEvent<HTMLFormElement>) => {
    e.preventDefault();
    onSubmit?.();
  };
  return (
    <main className="auth">
      <form className="dz lockcard" onSubmit={submit} noValidate>
        <div className="auth-brand">
          <Logo />
          <span>Finuance</span>
        </div>
        <h2>{title}</h2>
        <p>{lead}</p>
        {children}
      </form>
      <Toaster />
    </main>
  );
}
