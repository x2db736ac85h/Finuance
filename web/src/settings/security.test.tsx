import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import type { Schemas } from '../api/client';
import { encodeQr, qrPath } from '../lib/qr';
import { renderApp } from '../test/render';
import { API, db, problem, resetDb, server } from '../test/server';

beforeAll(() => {
  server.listen({ onUnhandledFrame: 'error' });
});
afterEach(() => {
  server.resetHandlers();
});
afterAll(() => {
  server.close();
});
beforeEach(() => {
  resetDb();
});

const SECRET = 'GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ';
const URI = `otpauth://totp/Finuance:alex@example.com?algorithm=SHA1&digits=6&issuer=Finuance&period=30&secret=${SECRET}`;
const CODES = ['aaaaa-bbbbb', 'ccccc-ddddd', 'eeeee-fffff'];

/** A server that keeps the state of one account's second factor. */
function factorServer(start: Partial<Schemas['SecondFactor']> = {}) {
  let factor: Schemas['SecondFactor'] = {
    enabled: false,
    available: true,
    recoveryCodes: 0,
    requiredForAdmins: false,
    ...start,
  };
  const calls: string[] = [];
  server.use(
    http.get(`${API}/me/second-factor`, () => HttpResponse.json(factor)),
    http.post(`${API}/me/second-factor`, () => {
      calls.push('start');
      return HttpResponse.json({ secret: SECRET, uri: URI });
    }),
    http.post(`${API}/me/second-factor/confirm`, async ({ request }) => {
      const { code } = (await request.json()) as { code: string };
      calls.push(`confirm ${code}`);
      if (code !== '123456')
        return problem(422, 'validation_failed', 'code: is not the current code from your authenticator app');
      factor = { ...factor, enabled: true, confirmedAt: '2026-11-19T10:00:00Z', recoveryCodes: CODES.length };
      db.user = { ...db.user, secondFactor: true, secondFactorRequired: false };
      return HttpResponse.json({ recoveryCodes: CODES });
    }),
    http.post(`${API}/me/second-factor/recovery-codes`, async ({ request }) => {
      const { code } = (await request.json()) as { code: string };
      calls.push(`codes ${code}`);
      return HttpResponse.json({ recoveryCodes: ['ggggg-hhhhh'] });
    }),
    http.post(`${API}/me/second-factor/disable`, async ({ request }) => {
      const body = (await request.json()) as { password: string; code: string };
      calls.push(`disable ${body.password} ${body.code}`);
      factor = { ...factor, enabled: false, recoveryCodes: 0 };
      return new HttpResponse(null, { status: 204 });
    }),
  );
  return calls;
}

describe('Settings → Security (ADR 0040)', () => {
  it('sets two-step sign-in up: the key as a QR code and as text, then the recovery codes once', async () => {
    const calls = factorServer();
    renderApp('/settings/security');
    fireEvent.click(await screen.findByRole('button', { name: 'Set up two-step sign-in' }, { timeout: 5000 }));
    const form = await screen.findByRole('form', { name: 'Set up two-step sign-in' });
    // the key, grouped for typing, and the same address as a QR code and as a link
    expect(within(form).getByLabelText('The key for your authenticator app').textContent).toBe(
      'GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ',
    );
    const qr = within(form).getByRole('img', { name: 'QR code for your authenticator app' });
    const expected = encodeQr(URI);
    if (!expected) throw new Error('no symbol');
    expect(qr.querySelector('path')?.getAttribute('d')).toBe(qrPath(expected, 4));
    expect(qr.querySelector('rect')?.getAttribute('fill')).toBe('#fff'); // dark on white in every theme
    expect(qr.getAttribute('viewBox')).toBe(`0 0 ${String(expected.size + 8)} ${String(expected.size + 8)}`);
    expect(within(form).getByRole('link', { name: 'Open it in your authenticator app' }).getAttribute('href')).toBe(
      URI,
    );

    // a wrong code changes nothing; the right one switches it on
    const code = within(form).getByLabelText('Code from your authenticator app');
    expect(within(form).getByRole<HTMLButtonElement>('button', { name: 'Switch it on' }).disabled).toBe(true);
    fireEvent.change(code, { target: { value: '000000' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Switch it on' }));
    expect(await screen.findByText(/is not the current code/, {}, { timeout: 3000 })).toBeTruthy();
    expect(screen.getByRole('form', { name: 'Set up two-step sign-in' })).toBeTruthy();
    fireEvent.change(code, { target: { value: '123456' } });
    fireEvent.click(within(form).getByRole('button', { name: 'Switch it on' }));

    const shown = await screen.findByRole('status', { name: 'Recovery codes' }, { timeout: 3000 });
    expect(
      within(shown)
        .getAllByRole('listitem')
        .map((li) => li.textContent),
    ).toEqual(CODES);
    expect(screen.queryByText(SECRET)).toBeNull(); // the key is gone from the page
    fireEvent.click(within(shown).getByRole('button', { name: 'I have saved them' }));
    expect(await screen.findByText(/3 recovery codes left/)).toBeTruthy();
    expect(screen.queryByText(CODES[0] ?? '')).toBeNull(); // and so are the codes
    expect(calls).toEqual(['start', 'confirm 000000', 'confirm 123456']);
  });

  it('makes new recovery codes and switches it off, each with a code', async () => {
    const calls = factorServer({ enabled: true, confirmedAt: '2026-11-19T10:00:00Z', recoveryCodes: 1 });
    renderApp('/settings/security');
    expect(await screen.findByText(/1 recovery code left/)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'New recovery codes' }));
    const codes = await screen.findByRole('form', { name: 'New recovery codes' });
    fireEvent.change(within(codes).getByLabelText('Code from your authenticator app'), { target: { value: '654321' } });
    fireEvent.click(within(codes).getByRole('button', { name: 'Make new codes' }));
    const shown = await screen.findByRole('status', { name: 'Recovery codes' }, { timeout: 3000 });
    expect(within(shown).getByText('ggggg-hhhhh')).toBeTruthy();
    fireEvent.click(within(shown).getByRole('button', { name: 'I have saved them' }));

    fireEvent.click(await screen.findByRole('button', { name: 'Switch it off' }));
    const off = await screen.findByRole('form', { name: 'Switch two-step sign-in off' });
    const submit = within(off).getByRole<HTMLButtonElement>('button', { name: 'Switch it off' });
    expect(submit.disabled).toBe(true); // needs both
    fireEvent.change(within(off).getByLabelText('Password'), { target: { value: 'my password' } });
    fireEvent.change(within(off).getByLabelText('Code or recovery code'), { target: { value: 'aaaaa-bbbbb' } });
    fireEvent.click(submit);
    expect(await screen.findByRole('button', { name: 'Set up two-step sign-in' })).toBeTruthy();
    expect(calls).toEqual(['codes 654321', 'disable my password aaaaa-bbbbb']);
  });

  it('says why it can not be set up, and that the instance asks administrators for it', async () => {
    factorServer({ available: false, requiredForAdmins: true });
    renderApp('/settings/security');
    expect(await screen.findByText(/requires two-step sign-in for administrators/)).toBeTruthy();
    expect(screen.getByText(/FINUANCE_INSTANCE_KEY/)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Set up two-step sign-in' })).toBeNull();
  });

  it('sends an administrator who must set it up from Administration to this page', async () => {
    factorServer({ requiredForAdmins: true });
    db.user = { ...db.user, isAdmin: true, secondFactorRequired: true };
    const { router } = renderApp('/admin/users');
    await waitFor(() => {
      expect(router.state.location.pathname).toBe('/settings/security');
    });
    expect(await screen.findByText(/Administration opens once you have set it up/)).toBeTruthy();
  });
});
