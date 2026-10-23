import { describe, expect, it } from 'vitest';

import { readEvents, type SseEvent } from './sse';

function stream(chunks: string[]): ReadableStream<Uint8Array> {
  const enc = new TextEncoder();
  return new ReadableStream({
    start(c) {
      for (const ch of chunks) c.enqueue(enc.encode(ch));
      c.close();
    },
  });
}

describe('readEvents', () => {
  it('splits events across chunk boundaries and skips comments', async () => {
    const got: SseEvent[] = [];
    await readEvents(
      stream([
        'event: con',
        'tent\ndata: {"delta":"Hel',
        'lo"}\n\n: keep-alive\n\n',
        'event: done\r\ndata: {}\r\n\r\n',
      ]),
      (e) => got.push(e),
    );
    expect(got).toEqual([
      { event: 'content', data: '{"delta":"Hello"}' },
      { event: 'done', data: '{}' },
    ]);
  });

  it('delivers a last event without a trailing blank line', async () => {
    const got: SseEvent[] = [];
    await readEvents(stream(['event: error\ndata: {"code":"x"}']), (e) => got.push(e));
    expect(got).toEqual([{ event: 'error', data: '{"code":"x"}' }]);
  });
});
