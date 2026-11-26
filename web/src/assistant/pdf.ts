// A PDF for the assistant (Phase 27): its first pages are drawn to pictures in the browser, which
// then go the way photos go (shrunk, JPEG, uploaded). Nothing of the PDF itself leaves the device,
// and a model that reads pictures needs no PDF support of its own.
//
// pdf.js (pdfjs-dist, Apache-2.0) is loaded only when a PDF arrives. It runs under the app's
// Content-Security-Policy: its worker is a file of this origin, started through a Trusted Types
// policy that allows that one address; WebAssembly decoders are off (the policy has no
// 'wasm-unsafe-eval'), so its JavaScript ones are used.

import { fitWithin, MAX_SIDE } from './photos';

/** How many pages of a PDF become pictures (a question takes three pictures at most). */
export const MAX_PDF_PAGES = 3;
const QUALITY = 0.85;

type PdfJs = typeof import('pdfjs-dist');
let ready: Promise<PdfJs> | null = null;

/** As much of the Trusted Types API as is used here (TypeScript's DOM types don't have it yet). */
type TrustedTypes = {
  createPolicy: (
    name: string,
    rules: { createScriptURL: (url: string) => string },
  ) => { createScriptURL: (url: string) => unknown };
};

/**
 * The worker's address as the page's policy wants it said (`require-trusted-types-for 'script'`):
 * a policy that passes exactly that one address. Browsers without Trusted Types take the string.
 */
function workerAddress(url: string): string {
  const tt = (window as { trustedTypes?: TrustedTypes }).trustedTypes;
  if (!tt) return url;
  const policy = tt.createPolicy('finuance-pdf', {
    createScriptURL: (u) => {
      if (u !== url) throw new TypeError('not the PDF worker');
      return u;
    },
  });
  // a TrustedScriptURL, which the Worker constructor accepts where it accepts a string
  return policy.createScriptURL(url) as string;
}

async function load(): Promise<PdfJs> {
  ready ??= (async () => {
    const [pdfjs, worker] = await Promise.all([
      // the "legacy" build: the same pdf.js with what browsers of the last few years lack filled in
      // (the default build uses language features only the newest have, and stops with a
      // TypeError elsewhere)
      import('pdfjs-dist/legacy/build/pdf.mjs'),
      // `?worker&url`: the worker built as a script of its own with a `.js` name. The package's file
      // is `.mjs`, which a web server may send as `application/octet-stream`; a browser then refuses
      // to run it and the worker never starts.
      import('pdfjs-dist/legacy/build/pdf.worker.min.mjs?worker&url'),
    ]);
    const port = new Worker(workerAddress(worker.default), { type: 'module' });
    // a worker that can't start (refused by the server or by the page's policy) says so only here;
    // without this the first PDF would wait for ever
    port.addEventListener('error', () => {
      workerFailed = true;
      ready = null;
    });
    pdfjs.GlobalWorkerOptions.workerPort = port;
    return pdfjs;
  })();
  return ready;
}

let workerFailed = false;
/** How long one PDF may take before it is given up (a damaged file can keep pdf.js busy). */
const PDF_TIMEOUT_MS = 30_000;

/** `work`, given up with a sentence for the person when the worker died or the time is over. */
async function guarded<T>(work: Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let poll: ReturnType<typeof setInterval> | undefined;
  const gaveUp = new Promise<never>((_, reject) => {
    const fail = () => {
      reject(new Error('This browser could not start reading the PDF. Take a picture of the page instead.'));
    };
    timer = setTimeout(fail, PDF_TIMEOUT_MS);
    poll = setInterval(() => {
      if (workerFailed) fail();
    }, 250);
  });
  try {
    return await Promise.race([work, gaveUp]);
  } finally {
    clearTimeout(timer);
    clearInterval(poll);
  }
}

/**
 * The first pages of a PDF as JPEGs, each at most MAX_SIDE px a side, with how many pages the
 * file has. Throws with a sentence for the person when the file can't be read (not a PDF,
 * protected by a password, damaged).
 */
export function pdfToImages(file: Blob, maxPages = MAX_PDF_PAGES): Promise<{ images: Blob[]; pages: number }> {
  workerFailed = false;
  return guarded(draw(file, maxPages));
}

async function draw(file: Blob, maxPages: number): Promise<{ images: Blob[]; pages: number }> {
  const pdfjs = await load();
  const task = pdfjs.getDocument({
    data: new Uint8Array(await file.arrayBuffer()),
    useWasm: false,
    useSystemFonts: true,
    enableXfa: false,
    stopAtErrors: false,
  });
  let doc;
  try {
    doc = await task.promise;
  } catch (e) {
    const name = e instanceof Error ? e.name : '';
    throw new Error(
      name === 'PasswordException'
        ? 'This PDF is protected by a password. Save a copy without one, or take a picture of the page.'
        : 'This file could not be read as a PDF.',
      { cause: e },
    );
  }
  try {
    const images: Blob[] = [];
    for (let n = 1; n <= Math.min(doc.numPages, maxPages); n++) {
      const page = await doc.getPage(n);
      const base = page.getViewport({ scale: 1 });
      const { w } = fitWithin(base.width, base.height, MAX_SIDE);
      // drawn larger than it prints when the page is small: a receipt's small type stays readable
      const viewport = page.getViewport({
        scale: Math.min(3, Math.max(w / base.width, MAX_SIDE / Math.max(base.width, base.height))),
      });
      const canvas = document.createElement('canvas');
      canvas.width = Math.ceil(viewport.width);
      canvas.height = Math.ceil(viewport.height);
      const g = canvas.getContext('2d');
      if (!g) throw new Error('This browser cannot prepare PDFs.');
      // a PDF page has no background of its own: white, as on paper
      g.fillStyle = '#ffffff';
      g.fillRect(0, 0, canvas.width, canvas.height);
      await page.render({ canvas, canvasContext: g, viewport }).promise;
      images.push(
        await new Promise<Blob>((resolve, reject) => {
          canvas.toBlob(
            (b) => {
              if (b) resolve(b);
              else reject(new Error('This browser cannot prepare PDFs.'));
            },
            'image/jpeg',
            QUALITY,
          );
        }),
      );
      page.cleanup();
    }
    return { images, pages: doc.numPages };
  } finally {
    void task.destroy();
  }
}
