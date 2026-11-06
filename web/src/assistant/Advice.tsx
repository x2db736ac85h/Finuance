// An article of the advice library (ADR 0034), opened from a search_advice lookup: the full text an
// administrator published, rendered with the chat's markdown (no HTML).

import { useQuery } from '@tanstack/react-query';

import { api, data } from '../api/client';
import { errorMessage } from '../lib/errors';
import { Modal } from '../ui/Modal';
import { Markdown } from './md';

export type AdviceRef = { slug: string; title: string; language: string };

/** The articles a search_advice lookup found, from what went back to the model. */
export function adviceRefs(result: string): AdviceRef[] {
  try {
    const r = JSON.parse(result) as { articles?: AdviceRef[] };
    return (r.articles ?? []).filter((a) => typeof a.slug === 'string' && typeof a.title === 'string');
  } catch {
    return [];
  }
}

export function AdviceWindow({ article, onClose }: { article: AdviceRef; onClose: () => void }) {
  const q = useQuery({
    queryKey: ['advice', article.slug, article.language],
    queryFn: async () =>
      data(
        await api.GET('/advice/{slug}', {
          params: { path: { slug: article.slug }, query: { language: article.language } },
        }),
      ),
  });
  return (
    <Modal
      open
      onClose={onClose}
      title={q.data?.title ?? article.title}
      sub="Advice library · general information, not financial advice"
    >
      <div className="advice-body">
        {q.data ? (
          <Markdown text={q.data.body} />
        ) : (
          <p className="hint">{q.isError ? errorMessage(q.error) : 'Loading…'}</p>
        )}
      </div>
    </Modal>
  );
}
