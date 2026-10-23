// What to do about an assistant error (PLAN task 13.7: error states with concrete fixes). Codes are
// the server's error kinds (llm), its problem codes, and the panel's own.

const FIXES: Record<string, string> = {
  unreachable:
    'Start the model server (LM Studio: Developer tab → Start server; Ollama: ollama serve), or ask your admin to check the connection.',
  auth: 'The model server rejected the API key. Ask your admin to check the connection’s key.',
  tools_rejected: 'This model can’t look up data with tools. Choose Plan mode in the assistant settings.',
  context_too_long: 'The conversation no longer fits this model. Start a new chat, or lower the rows per lookup.',
  model_not_found: 'The model isn’t available. Load it in LM Studio (or ollama pull it), or pick another model.',
  timeout: 'The model took too long. Try a smaller model, or turn thinking off.',
  rate_limited: 'Too many requests. Wait a moment and ask again.',
  run_in_progress: 'Another answer is still being written (maybe in another tab). Wait for it or stop it.',
  assistant_busy: 'The assistant is busy with other questions. Try again in a moment.',
  assistant_unavailable: 'Check the connection in the assistant settings.',
  consent_required: 'Agree to send data to this cloud connection, or choose a local one.',
  interrupted: 'The connection dropped. Ask again.',
};

/** The advice for an error code, or undefined when the message says it all. */
export function fixFor(code: string): string | undefined {
  return FIXES[code];
}
