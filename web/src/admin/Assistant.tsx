// Administration → Assistant (Phase 20): the instructions added to every system prompt, and the
// advice library the assistant searches (ADR 0034). Articles are markdown, shown with the chat's
// renderer; the starter articles arrive as drafts for an administrator to read and publish.

import { useState } from 'react';

import {
  useAddStarterAdvice,
  useAdviceArticle,
  useAdviceList,
  useCreateAdvice,
  useDeleteAdvice,
  useInstanceSettings,
  usePutInstanceSettings,
  useUpdateAdvice,
  type AdviceArticle,
  type AdviceInput,
  type InstanceSettings,
} from '../api/hooks/admin';
import { Markdown } from '../assistant/md';
import { fmtDateTime } from '../lib/dates';
import { errorMessage } from '../lib/errors';
import { Button } from '../ui/Button';
import { Select, TextInput } from '../ui/inputs';
import { toast } from '../ui/toastStore';

const MAX_INSTRUCTIONS = 2000;

export function AssistantAdmin() {
  const [editing, setEditing] = useState<string | null>(null);
  return (
    <>
      <Instructions />
      <Library editing={editing} onEdit={setEditing} />
      {editing && <ArticleEditor key={editing} id={editing === 'new' ? null : editing} onDone={setEditing} />}
    </>
  );
}

function Instructions() {
  const settings = useInstanceSettings();
  if (!settings.data)
    return <div className="set-card">{settings.isError ? errorMessage(settings.error) : 'Loading…'}</div>;
  return <InstructionsForm key={settings.data.version} doc={settings.data} />;
}

function InstructionsForm({ doc }: { doc: InstanceSettings }) {
  const put = usePutInstanceSettings();
  const [text, setText] = useState(doc.assistant?.instructions ?? '');
  return (
    <div className="set-card">
      <div className="lbl">Instructions for the assistant</div>
      <p className="hint">
        Added to every question's instructions on this instance, after the app's own rules (which they can't change): a
        tone, a language, articles to point to. Every user's model reads them.
      </p>
      <textarea
        className="adm-text"
        aria-label="Instructions for the assistant"
        rows={5}
        maxLength={MAX_INSTRUCTIONS}
        value={text}
        onChange={(e) => {
          setText(e.target.value);
        }}
      />
      <div className="set-actions">
        <span className="hint">
          {text.length} / {MAX_INSTRUCTIONS}
        </span>
        <Button
          variant="primary"
          size="sm"
          disabled={put.isPending || text === (doc.assistant?.instructions ?? '')}
          onClick={() => {
            void put.mutateAsync({ ...doc, assistant: { instructions: text.trim() } }).then(
              () => {
                toast('Saved the instructions');
              },
              (e: unknown) => {
                toast(errorMessage(e), { kind: 'error' });
              },
            );
          }}
        >
          Save
        </Button>
      </div>
    </div>
  );
}

function Library({ editing, onEdit }: { editing: string | null; onEdit: (id: string | null) => void }) {
  const list = useAdviceList();
  const starter = useAddStarterAdvice();
  return (
    <div className="set-card">
      <div className="lbl">Advice library</div>
      <p className="hint">
        Short articles the assistant searches for general money questions and names in its answer. Only published ones
        are found. They are general information, not financial advice; the starter articles say so at the end.
      </p>
      {list.isError && <p className="set-err">{errorMessage(list.error)}</p>}
      {list.data?.length === 0 && <p className="hint">No articles yet.</p>}
      <ul className="set-list">
        {list.data?.map((a) => (
          <li key={a.id} className={editing === a.id ? 'on' : undefined}>
            <div className="set-li-main">
              <b>{a.title}</b>
              <span className="tag">{a.language}</span>
              <span className="tag src">{a.status === 'published' ? 'Published' : 'Draft'}</span>
              <span className="hint">
                {a.slug} · {a.bodyLength.toLocaleString()} characters · changed {fmtDateTime(a.updatedAt)}
              </span>
            </div>
            <Button
              size="sm"
              onClick={() => {
                onEdit(a.id);
              }}
            >
              Edit
            </Button>
          </li>
        ))}
      </ul>
      <div className="set-actions">
        <Button
          size="sm"
          disabled={starter.isPending}
          onClick={() => {
            starter.mutate(undefined, {
              onSuccess: (r) => {
                toast(
                  r.added
                    ? `Added ${String(r.added)} starter articles as drafts`
                    : 'Every starter article is in the library already',
                );
              },
              onError: (e) => {
                toast(errorMessage(e), { kind: 'error' });
              },
            });
          }}
        >
          Add the starter articles
        </Button>
        <Button
          variant="primary"
          size="sm"
          onClick={() => {
            onEdit('new');
          }}
        >
          New article
        </Button>
      </div>
    </div>
  );
}

const EMPTY: AdviceInput = { slug: '', language: 'en', title: '', body: '', tags: [], status: 'draft' };

function ArticleEditor({ id, onDone }: { id: string | null; onDone: (id: string | null) => void }) {
  const article = useAdviceArticle(id);
  if (id && !article.data)
    return <div className="set-card">{article.isError ? errorMessage(article.error) : 'Loading…'}</div>;
  return <ArticleForm article={article.data} onDone={onDone} />;
}

function slugOf(title: string): string {
  return title
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64);
}

function ArticleForm({ article, onDone }: { article: AdviceArticle | undefined; onDone: (id: string | null) => void }) {
  const create = useCreateAdvice();
  const update = useUpdateAdvice();
  const del = useDeleteAdvice();
  const [a, setA] = useState<AdviceInput>(article ? { ...article } : EMPTY);
  const [tags, setTags] = useState((article?.tags ?? []).join(', '));
  const [preview, setPreview] = useState(false);
  const busy = create.isPending || update.isPending || del.isPending;
  const fail = (e: unknown) => {
    toast(errorMessage(e), { kind: 'error' });
  };
  const save = () => {
    const body: AdviceInput = {
      slug: a.slug || slugOf(a.title),
      language: a.language,
      title: a.title,
      body: a.body,
      status: a.status,
      tags: tags
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean),
    };
    if (article) {
      update.mutate(
        { id: article.id, version: article.version, ...body },
        {
          onSuccess: () => {
            toast(`Saved ${body.title}`);
            onDone(null);
          },
          onError: fail,
        },
      );
    } else {
      create.mutate(body, {
        onSuccess: () => {
          toast(`Saved ${body.title}`);
          onDone(null);
        },
        onError: fail,
      });
    }
  };
  return (
    <form
      className="set-card"
      aria-label="Article"
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <div className="lbl">{article ? `Edit ${article.title}` : 'New article'}</div>
      <label className="st-col">
        Title
        <TextInput
          value={a.title}
          maxLength={200}
          onChange={(e) => {
            setA({ ...a, title: e.target.value });
          }}
        />
      </label>
      <div className="set-actions adm-toolbar">
        <label className="st-col">
          Address (slug)
          <TextInput
            mono
            value={a.slug}
            placeholder={slugOf(a.title)}
            maxLength={64}
            onChange={(e) => {
              setA({ ...a, slug: e.target.value });
            }}
          />
        </label>
        <label className="st-col">
          Language
          <TextInput
            value={a.language}
            maxLength={2}
            onChange={(e) => {
              setA({ ...a, language: e.target.value.toLowerCase() });
            }}
          />
        </label>
        <label className="st-col">
          Status
          <Select
            value={a.status}
            options={[
              { value: 'draft', label: 'Draft' },
              { value: 'published', label: 'Published' },
            ]}
            onChange={(status) => {
              setA({ ...a, status });
            }}
          />
        </label>
      </div>
      <label className="st-col">
        Tags <span className="hint">(comma-separated, searched like the title)</span>
        <TextInput
          value={tags}
          onChange={(e) => {
            setTags(e.target.value);
          }}
        />
      </label>
      <div className="set-actions">
        <span className="lbl">Text (markdown)</span>
        <Button
          size="sm"
          onClick={() => {
            setPreview(!preview);
          }}
        >
          {preview ? 'Edit the text' : 'Preview'}
        </Button>
      </div>
      {preview ? (
        <div className="advice-body adm-preview">
          <Markdown text={a.body} />
        </div>
      ) : (
        <textarea
          className="adm-text"
          aria-label="Text (markdown)"
          rows={14}
          maxLength={20000}
          value={a.body}
          onChange={(e) => {
            setA({ ...a, body: e.target.value });
          }}
        />
      )}
      <div className="set-actions">
        {article && (
          <Button
            variant="danger"
            size="sm"
            disabled={busy}
            onClick={() => {
              del.mutate(article.id, {
                onSuccess: () => {
                  toast(`Deleted ${article.title}`);
                  onDone(null);
                },
                onError: fail,
              });
            }}
          >
            Delete
          </Button>
        )}
        <Button
          size="sm"
          onClick={() => {
            onDone(null);
          }}
        >
          Cancel
        </Button>
        <Button type="submit" variant="primary" size="sm" disabled={busy || !a.title.trim()}>
          Save
        </Button>
      </div>
    </form>
  );
}
