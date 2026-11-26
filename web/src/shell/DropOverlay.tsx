// Drag & drop of files. Since Phase 27 only some places take them (`data-drop` on the element):
//   "files": the transactions list, the start screen and the sidebar's drop zone. A statement
//            (CSV, TSV, TXT) dropped there is imported; a picture or a PDF goes to the assistant.
//   "chat":  the assistant's panel, for pictures and PDFs.
// Anywhere else (the charts pane, Settings, …) a drop does nothing; the browser is still kept from
// opening the file. The overlay covers the place the files would land in and says what happens.
// Only drags that carry files count, so reordering chart cards never triggers any of this.

import { useEffect, useEffectEvent, useRef, useState } from 'react';

import { dragKind, dropKind, useChatDrops } from '../assistant/drops';
import { plural } from '../lib/format';
import { useUi } from '../stores/uiStore';
import { Icon } from '../ui/Icon';
import { toast } from '../ui/toastStore';

const hasFiles = (e: DragEvent) => [...(e.dataTransfer?.types ?? [])].includes('Files');

type Zone = { el: HTMLElement; kind: 'files' | 'chat' };

function zoneOf(e: DragEvent): Zone | null {
  const el = e.target instanceof Element ? e.target.closest<HTMLElement>('[data-drop]') : null;
  const kind = el?.dataset['drop'];
  return el && (kind === 'files' || kind === 'chat') ? { el, kind } : null;
}

/** What the overlay says while files hover over a zone. */
export function dropWords(
  zone: 'files' | 'chat',
  dragged: 'chat' | 'import' | 'mixed',
  assistant: boolean,
): [string, string] {
  if (zone === 'chat' || (dragged === 'chat' && assistant))
    return ['Drop for the assistant', 'A picture or a PDF of a receipt — 10 MB a file at most'];
  if (dragged === 'mixed' && assistant)
    return ['Drop to open', 'Statements are imported; pictures and PDFs go to the assistant'];
  return ['Drop to import', 'CSV, TSV or TXT — multiple files at once'];
}

type Props = {
  onFiles: (files: readonly File[]) => void;
  /** The assistant is switched on for this instance, so pictures and PDFs have somewhere to go. */
  assistant: boolean;
};

export function DropOverlay({ onFiles, assistant }: Props) {
  const box = useRef<HTMLDivElement>(null);
  const [words, setWords] = useState<[string, string]>(['Drop to import', '']);
  const sort = useEffectEvent((zone: Zone['kind'], files: readonly File[]) => {
    const statements = files.filter((f) => dropKind(f) === 'import');
    const forChat = files.filter((f) => ['photo', 'pdf'].includes(dropKind(f)));
    const neither = files.length - statements.length - forChat.length;
    if (neither) toast(`Skipped ${plural(neither, 'file')} that is no statement, picture or PDF.`, { kind: 'error' });
    if (statements.length) {
      if (zone === 'chat') toast('Drop statements on the transactions list to import them.', { kind: 'error' });
      else onFiles(statements);
    }
    if (!forChat.length) return;
    if (!assistant) {
      toast('Pictures and PDFs are read by the assistant, which is switched off here.', { kind: 'error' });
      return;
    }
    useChatDrops.getState().give(forChat);
    useUi.getState().setAiOpen(true);
  });
  const say = useEffectEvent((zone: Zone, e: DragEvent) => {
    const types = [...(e.dataTransfer?.items ?? [])].filter((i) => i.kind === 'file').map((i) => i.type);
    const next = dropWords(zone.kind, dragKind(types), assistant);
    setWords((w) => (w[0] === next[0] && w[1] === next[1] ? w : next));
    // the overlay lies over the zone itself (set through the CSSOM, which the CSP allows)
    const r = zone.el.getBoundingClientRect();
    const s = box.current?.style;
    s?.setProperty('left', `${r.left.toFixed(0)}px`);
    s?.setProperty('top', `${r.top.toFixed(0)}px`);
    s?.setProperty('width', `${r.width.toFixed(0)}px`);
    s?.setProperty('height', `${r.height.toFixed(0)}px`);
  });
  useEffect(() => {
    const body = document.body.classList;
    const stop = () => {
      body.remove('dragging');
    };
    const over = (e: DragEvent) => {
      if (!hasFiles(e) || !e.dataTransfer) return;
      // always: without it the browser would open a dropped file in place of the app
      e.preventDefault();
      const zone = zoneOf(e);
      e.dataTransfer.dropEffect = zone ? 'copy' : 'none';
      if (zone) {
        say(zone, e);
        body.add('dragging');
      } else stop();
    };
    const leave = (e: DragEvent) => {
      // the drag left the window (no element it went to)
      if (hasFiles(e) && !e.relatedTarget) stop();
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      stop();
      const zone = zoneOf(e);
      if (zone && e.dataTransfer?.files.length) sort(zone.kind, [...e.dataTransfer.files]);
    };
    window.addEventListener('dragenter', over);
    window.addEventListener('dragover', over);
    window.addEventListener('dragleave', leave);
    window.addEventListener('drop', drop);
    window.addEventListener('dragend', stop);
    return () => {
      window.removeEventListener('dragenter', over);
      window.removeEventListener('dragover', over);
      window.removeEventListener('dragleave', leave);
      window.removeEventListener('drop', drop);
      window.removeEventListener('dragend', stop);
      stop();
    };
  }, []);
  return (
    <div className="overlay zone" aria-hidden="true" ref={box}>
      <div className="overlay-card">
        <div className="dz-ico">
          <Icon name="upload" />
        </div>
        <b>{words[0]}</b>
        <span>{words[1]}</span>
      </div>
    </div>
  );
}
