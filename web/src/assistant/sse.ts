// Reads a text/event-stream body (the assistant's answer, PLAN §6.3): `event:` and `data:` lines,
// a blank line ending each event, comment lines (the server's keep-alive) ignored.

export type SseEvent = { event: string; data: string };

export async function readEvents(body: ReadableStream<Uint8Array>, onEvent: (e: SseEvent) => void): Promise<void> {
  const reader = body.getReader();
  const dec = new TextDecoder();
  let buf = '';
  let event = '';
  let data: string[] = [];
  const line = (l: string) => {
    if (l === '') {
      if (data.length) onEvent({ event: event || 'message', data: data.join('\n') });
      event = '';
      data = [];
    } else if (l.startsWith(':')) {
      // comment
    } else if (l.startsWith('event:')) {
      event = l.slice(6).trim();
    } else if (l.startsWith('data:')) {
      data.push(l.slice(5).replace(/^ /, ''));
    }
  };
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += dec.decode(value, { stream: true });
    let i: number;
    while ((i = buf.indexOf('\n')) >= 0) {
      line(buf.slice(0, i).replace(/\r$/, ''));
      buf = buf.slice(i + 1);
    }
  }
  buf += dec.decode();
  if (buf) line(buf.replace(/\r$/, ''));
  line('');
}
