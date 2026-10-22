// Entry points for opening CSV files (PLAN task 9.1): ⌘/Ctrl O, the empty state's Browse button,
// the sidebar's mini drop zone and drag & drop all end in importFiles (import/queue.ts).

export { importFiles } from '../import/queue';

let picker: HTMLInputElement | null = null;

/** Ref callback for the app's hidden `<input type=file>`. */
export function registerPicker(el: HTMLInputElement | null): void {
  picker = el;
}

export function openFilePicker(): void {
  picker?.click();
}

export const FILE_ACCEPT = '.csv,.tsv,.txt,text/csv,text/plain';
