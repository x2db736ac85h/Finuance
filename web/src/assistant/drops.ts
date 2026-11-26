// Files for the assistant from outside its panel (Phase 27): a picture or a PDF dropped on the
// transactions list. The shell sorts dropped files, opens the panel and leaves them here; the panel
// takes them as if they had been picked with its own button.

import { create } from 'zustand';

/** The largest file the assistant takes (the original, before a picture is shrunk or a PDF drawn). */
export const MAX_CHAT_FILE_BYTES = 10 * 1024 * 1024;

export type DropKind = 'import' | 'photo' | 'pdf' | 'other';

/** What a dropped file is for: a statement to import, something the assistant reads, or neither. */
export function dropKind(f: { name: string; type: string }): DropKind {
  if (f.type === 'application/pdf' || /\.pdf$/i.test(f.name)) return 'pdf';
  if (f.type.startsWith('image/') || /\.(jpe?g|png|webp|gif|heic|heif|avif|bmp)$/i.test(f.name)) return 'photo';
  if (/\.(csv|tsv|txt)$/i.test(f.name) || f.type.startsWith('text/')) return 'import';
  return 'other';
}

/** The same for a drag still in the air, where only the kinds of the files are known, not their names. */
export function dragKind(types: readonly string[]): 'chat' | 'import' | 'mixed' {
  const known = types.filter(Boolean);
  if (!known.length) return 'mixed';
  const chat = known.filter((t) => t === 'application/pdf' || t.startsWith('image/')).length;
  if (chat === known.length) return 'chat';
  return chat ? 'mixed' : 'import';
}

type Drops = {
  files: File[];
  /** Files for the assistant's next question. */
  give: (files: readonly File[]) => void;
  /** The waiting files, which are then no longer waiting. */
  take: () => File[];
};

export const useChatDrops = create<Drops>()((set, get) => ({
  files: [],
  give: (files) => {
    set((s) => ({ files: [...s.files, ...files] }));
  },
  take: () => {
    const files = get().files;
    if (files.length) set({ files: [] });
    return files;
  },
}));
