// Entry points for opening CSV files: ⌘/Ctrl O, the empty state's Browse button, the sidebar's mini
// drop zone and drag & drop all end in importFiles. The upload itself is Phase 9 (import preview).

import { toast } from '../ui/toastStore';

let picker: HTMLInputElement | null = null;

/** Ref callback for the app's hidden `<input type=file>`. */
export function registerPicker(el: HTMLInputElement | null): void {
  picker = el;
}

export function openFilePicker(): void {
  picker?.click();
}

export const FILE_ACCEPT = '.csv,.tsv,.txt,text/csv,text/plain';

export function importFiles(files: FileList | readonly File[]): void {
  if (!files.length) return;
  toast("Importing files isn't available yet; it arrives with the import preview.");
}
