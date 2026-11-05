// Administration → Developer (FINUANCE_DEVELOPER_MODE, ADR 0025): the build, the source repository
// and links to the tools around the instance, each with a note on how to get in. Everything shown
// comes from GET /admin/developer (administrators only, 404 while the mode is off); the bundle holds
// no URL or note. Notes and commands are plain text, links open in a new tab without a referrer.

import { useEffect, useId, useRef, useState } from 'react';

import { useDeveloperInfo, type DeveloperInfo } from '../api/hooks/admin';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { Icon } from '../ui/Icon';
import { toast } from '../ui/toastStore';

type Link = DeveloperInfo['links'][number];

export function DeveloperAdmin() {
  const dev = useDeveloperInfo();
  const d = dev.data;
  if (!d) return <div className="set-card">{dev.isError ? errorMessage(dev.error) : 'Loading…'}</div>;
  const b = d.build;
  const built = Number.isNaN(Date.parse(b.date)) ? b.date : fmtDateTime(b.date);
  const behind = b.schemaVersion < b.expectedSchemaVersion;
  const groups = new Map<string, Link[]>();
  for (const l of d.links) groups.set(l.group, [...(groups.get(l.group) ?? []), l]);
  return (
    <>
      <div className="set-card">
        <div className="lbl">Build</div>
        <dl className="dev-facts">
          <dt>Version</dt>
          <dd>
            {d.source?.releaseUrl ? <ExternalLink href={d.source.releaseUrl}>{b.version}</ExternalLink> : b.version}
          </dd>
          <dt>Commit</dt>
          <dd className="mono">
            {d.source?.commitUrl ? <ExternalLink href={d.source.commitUrl}>{b.commit}</ExternalLink> : b.commit}
          </dd>
          <dt>Built</dt>
          <dd>{built}</dd>
          <dt>Go</dt>
          <dd className="mono">{b.goVersion}</dd>
          <dt>Environment</dt>
          <dd>{b.environment}</dd>
          <dt>Answered by</dt>
          <dd className="mono">{b.instance}</dd>
          <dt>Database schema</dt>
          <dd className={behind ? 'dev-warn' : undefined}>
            {b.schemaVersion}
            {behind ? ` (behind: the build expects ${b.expectedSchemaVersion})` : ''}
          </dd>
          {d.source && (
            <>
              <dt>Source</dt>
              <dd>
                <ExternalLink href={d.source.url}>{d.source.url}</ExternalLink>
              </dd>
            </>
          )}
        </dl>
      </div>
      {[...groups].map(([group, links]) => (
        <div key={group} className="set-card">
          <div className="lbl">{group}</div>
          <ul className="dev-links">
            {links.map((l) => (
              <li key={`${l.name} ${l.url}`}>
                <ExternalLink href={l.url}>{l.name}</ExternalLink>
                <span className="hint mono">{l.url}</span>
                {(l.note !== undefined || l.commands !== undefined) && <HowToGetIn link={l} />}
              </li>
            ))}
          </ul>
        </div>
      ))}
      {d.links.length === 0 && (
        <p className="hint">
          No links configured: list them in the developer links file (<span className="mono">developer.links</span> in
          the Helm chart; docs/admin.md).
        </p>
      )}
    </>
  );
}

function ExternalLink({ href, children }: { href: string; children: React.ReactNode }) {
  // the server only sends http(s) URLs; checked again so no other scheme can ever become a link
  if (!/^https?:\/\//i.test(href)) return <>{children}</>;
  return (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

/**
 * The ⓘ button: the note and the commands open on hover and on keyboard focus (a click keeps it open
 * on touch screens), and stay open while the pointer or focus is inside, so Copy can be reached.
 * Escape closes it.
 */
function HowToGetIn({ link }: { link: Link }) {
  const [open, setOpen] = useState(false);
  const [pinned, setPinned] = useState(false);
  const wrap = useRef<HTMLSpanElement>(null);
  // closing waits a moment, so the pointer can cross from the button into the note
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);
  const id = useId();
  const show = () => {
    clearTimeout(timer.current);
    setOpen(true);
  };
  const close = () => {
    clearTimeout(timer.current);
    setOpen(false);
    setPinned(false);
  };
  useEffect(
    () => () => {
      clearTimeout(timer.current);
    },
    [],
  );
  return (
    <span
      ref={wrap}
      className="dev-info"
      onPointerEnter={show}
      onPointerLeave={() => {
        if (pinned || wrap.current?.contains(document.activeElement)) return;
        clearTimeout(timer.current);
        timer.current = setTimeout(() => {
          setOpen(false);
        }, 150);
      }}
      onFocus={show}
      onBlur={(e) => {
        if (!wrap.current?.contains(e.relatedTarget)) close();
      }}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && open) {
          e.stopPropagation();
          close();
        }
      }}
    >
      <button
        type="button"
        className="ib sm"
        aria-label={`How to get into ${link.name}`}
        aria-expanded={open}
        aria-controls={id}
        onClick={() => {
          setPinned(!pinned);
          setOpen(!pinned);
        }}
      >
        <Icon name="info" />
      </button>
      {open && (
        <span id={id} className="dev-pop" role="group" aria-label={`How to get into ${link.name}`}>
          {link.note && <span className="dev-note">{link.note}</span>}
          {link.commands?.map((command, i) => (
            <span key={i} className="dev-cmd">
              <code className="mono">{command}</code>
              <Button
                size="sm"
                icon="copy"
                aria-label={`Copy command ${i + 1} for ${link.name}`}
                onClick={() => {
                  void navigator.clipboard.writeText(command).then(() => {
                    toast('Copied the command');
                  });
                }}
              >
                Copy
              </Button>
            </span>
          ))}
        </span>
      )}
    </span>
  );
}
