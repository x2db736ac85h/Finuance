// Drag & drop anywhere (legacy): the overlay shows while files are dragged over the window. A depth
// counter avoids flicker as the drag crosses child elements, and only drags that carry files count,
// so reordering chart cards never triggers it.

import { useEffect, useEffectEvent } from 'react';

import { Icon } from '../ui/Icon';

const hasFiles = (e: DragEvent) => [...(e.dataTransfer?.types ?? [])].includes('Files');

export function DropOverlay({ onFiles }: { onFiles: (files: FileList) => void }) {
  const dropped = useEffectEvent(onFiles);
  useEffect(() => {
    let depth = 0;
    const body = document.body.classList;
    const enter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth++;
      body.add('dragging');
    };
    const over = (e: DragEvent) => {
      if (!hasFiles(e) || !e.dataTransfer) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = 'copy';
    };
    const leave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth = Math.max(0, depth - 1);
      if (!depth) body.remove('dragging');
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      body.remove('dragging');
      if (e.dataTransfer?.files.length) dropped(e.dataTransfer.files);
    };
    window.addEventListener('dragenter', enter);
    window.addEventListener('dragover', over);
    window.addEventListener('dragleave', leave);
    window.addEventListener('drop', drop);
    return () => {
      window.removeEventListener('dragenter', enter);
      window.removeEventListener('dragover', over);
      window.removeEventListener('dragleave', leave);
      window.removeEventListener('drop', drop);
      body.remove('dragging');
    };
  }, []);
  return (
    <div className="overlay" aria-hidden="true">
      <div className="overlay-card">
        <div className="dz-ico">
          <Icon name="upload" />
        </div>
        <b>Drop to open</b>
        <span>CSV, TSV or TXT — multiple files at once</span>
      </div>
    </div>
  );
}
