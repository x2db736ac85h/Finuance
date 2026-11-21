// Vitest setup. jsdom lacks matchMedia and ResizeObserver; tests that care stub them themselves.
import { cleanup } from '@testing-library/react';
import { File as NodeFile } from 'node:buffer';
import { afterEach } from 'vitest';

import { useToasts } from '../ui/toastStore';

if (typeof window.matchMedia !== 'function') {
  Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
      matches: false,
      media: query,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    }),
  });
}

// jsdom has no ResizeObserver: elements are measured once (useSize) and never again.
if (typeof window.ResizeObserver !== 'function') {
  window.ResizeObserver = class {
    observe() {
      return undefined;
    }
    unobserve() {
      return undefined;
    }
    disconnect() {
      return undefined;
    }
  };
}

// Node 26's Request can't read jsdom's FormData: a file part arrives as the text "[object Blob]"
// (jsdom's Blob is not Node's, and Node reads only its own). openapi-fetch builds every request with
// the global Request, so a request with such a body writes the multipart body itself here, from the
// form's own values. Test-only: in a browser, FormData and Request come from the same place.
const NodeRequest = globalThis.Request;
const JsdomFormData = globalThis.FormData;
function multipart(form: FormData): { body: ReadableStream<Uint8Array>; type: string } {
  const boundary = `----finuance-test-${Math.random().toString(16).slice(2)}`;
  const text = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start: async (out) => {
      for (const [name, value] of form) {
        const head =
          typeof value === 'string'
            ? `Content-Disposition: form-data; name="${name}"\r\n`
            : `Content-Disposition: form-data; name="${name}"; filename="${value.name}"\r\nContent-Type: ${value.type || 'application/octet-stream'}\r\n`;
        out.enqueue(text.encode(`--${boundary}\r\n${head}\r\n`));
        out.enqueue(typeof value === 'string' ? text.encode(value) : new Uint8Array(await value.arrayBuffer()));
        out.enqueue(text.encode('\r\n'));
      }
      out.enqueue(text.encode(`--${boundary}--\r\n`));
      out.close();
    },
  });
  return { body, type: `multipart/form-data; boundary=${boundary}` };
}
function withNodeBody(init?: RequestInit): RequestInit | undefined {
  if (!(init?.body instanceof JsdomFormData)) return init;
  const { body, type } = multipart(init.body);
  const headers = new Headers(init.headers);
  headers.set('Content-Type', type);
  // a stream body needs duplex: 'half' in Node
  return { ...init, body, headers, duplex: 'half' } as RequestInit;
}
// …and reading such a body back (a handler's request.formData()) makes Node's own Files, which it
// then checks against the global File: jsdom's, while a test runs. Node's is the global meanwhile.
// from the prototype chain: Node 22 defines it on a parent of Request.prototype
const nodeFormData: (this: Request) => Promise<FormData> = Reflect.get(NodeRequest.prototype, 'formData');
NodeRequest.prototype.formData = async function formData(this: Request): Promise<FormData> {
  const jsdomFile = globalThis.File;
  globalThis.File = NodeFile as unknown as typeof File;
  try {
    return await nodeFormData.call(this);
  } finally {
    globalThis.File = jsdomFile;
  }
};
globalThis.Request = class Request extends NodeRequest {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(input, withNodeBody(init));
  }
};

// Without vitest globals, Testing Library can't register its own cleanup.
afterEach(() => {
  cleanup();
  useToasts.setState({ queue: [] });
});
