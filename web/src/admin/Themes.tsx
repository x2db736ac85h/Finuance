// Administration → Colour themes (Phase 19, ADR 0026): themes made here start as drafts; an
// approved theme is offered to everyone next to the four built-in ones. Approval needs every text
// colour to reach WCAG AA (4.5:1) on its surfaces in both variants, checked here while editing and
// again by the server. Themes are colours only (hex), never CSS.

import { useState } from 'react';

import {
  useAdminThemes,
  useCreateTheme,
  useDeleteTheme,
  useSetThemeStatus,
  useUpdateTheme,
  type AdminTheme,
} from '../api/hooks/admin';
import { ApiError } from '../api/client';
import { errorMessage } from '../lib/errors';
import {
  BUILTIN_THEMES,
  HEX_COLOUR,
  THEME_KEYS,
  completeTokens,
  contrastReport,
  type Mode,
  type ThemeTokens,
} from '../styles/themes';
import { Button } from '../ui/Button';
import { Select, TextInput } from '../ui/inputs';
import { toast } from '../ui/toastStore';

const STATUS: Record<AdminTheme['status'], string> = {
  draft: 'draft',
  approved: 'offered to everyone',
  retired: 'retired',
};

/** What each token colours, for the editor's labels. */
const KEY_LABEL: Record<string, string> = {
  bg: 'Page background',
  panel: 'Panels',
  elev: 'Menus and cards',
  input: 'Input fields',
  border: 'Borders',
  text: 'Text',
  muted: 'Secondary text',
  accent: 'Accent (links, selection)',
  'accent-solid': 'Filled buttons',
  'on-accent': 'Text on filled buttons',
  'accent-soft': 'Accent tint (chips)',
  sel: 'Selected row tint',
  pos: 'Money in',
  neg: 'Money out',
  'logo-a': 'Logo, top',
  'logo-b': 'Logo, bottom',
};

function slugOf(name: string): string {
  return name
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32);
}

export function ThemesAdmin() {
  const themes = useAdminThemes();
  const [editing, setEditing] = useState<string | null>(null);
  const current = themes.data?.find((t) => t.id === editing);
  return (
    <>
      <NewTheme onCreated={setEditing} />
      <div className="set-card">
        <div className="lbl">Themes made here</div>
        {themes.isError && <p className="set-err">{errorMessage(themes.error)}</p>}
        {themes.data?.length === 0 && <p className="hint">None yet. Start one from a built-in theme above.</p>}
        <ul className="set-list">
          {themes.data?.map((t) => (
            <ThemeRow key={t.id} theme={t} editing={editing === t.id} onEdit={setEditing} />
          ))}
        </ul>
      </div>
      {current && <ThemeEditor key={`${current.id}:${String(current.version)}`} theme={current} />}
    </>
  );
}

function NewTheme({ onCreated }: { onCreated: (id: string) => void }) {
  const create = useCreateTheme();
  const [base, setBase] = useState('indigo');
  const [name, setName] = useState('');
  const submit = () => {
    const from = BUILTIN_THEMES.find((t) => t.id === base) ?? BUILTIN_THEMES[0];
    if (!from) return;
    create.mutate(
      { slug: slugOf(name), name: name.trim(), basedOn: from.id as 'indigo', tokens: structuredClone(from.tokens) },
      {
        onSuccess: (t) => {
          setName('');
          onCreated(t.id);
          toast(`Created the draft ${t.name}`);
        },
        onError: (e) => {
          toast(errorMessage(e), { kind: 'error' });
        },
      },
    );
  };
  return (
    <form
      className="set-card"
      onSubmit={(e) => {
        e.preventDefault();
        submit();
      }}
    >
      <div className="lbl">New theme</div>
      <p className="hint">Start from a built-in theme and change its colours. Users only see it once you approve it.</p>
      <div className="set-actions adm-toolbar">
        <label className="st-col">
          Start from
          <Select
            value={base}
            options={BUILTIN_THEMES.map((t) => ({ value: t.id, label: t.name }))}
            onChange={setBase}
          />
        </label>
        <label className="st-col">
          Name
          <TextInput
            value={name}
            maxLength={40}
            onChange={(e) => {
              setName(e.target.value);
            }}
          />
        </label>
        <Button type="submit" variant="primary" size="sm" disabled={!slugOf(name) || create.isPending}>
          Create draft
        </Button>
      </div>
    </form>
  );
}

function ThemeRow({
  theme,
  editing,
  onEdit,
}: {
  theme: AdminTheme;
  editing: boolean;
  onEdit: (id: string | null) => void;
}) {
  const status = useSetThemeStatus();
  const del = useDeleteTheme();
  const fail = (e: unknown) => {
    toast(errorMessage(e), { kind: 'error' });
  };
  const set = (to: AdminTheme['status'], done: string) => {
    status.mutate(
      { id: theme.id, body: { version: theme.version, status: to } },
      {
        onSuccess: () => {
          toast(done);
        },
        onError: fail,
      },
    );
  };
  return (
    <li>
      <div className="set-li-main">
        <b>{theme.name}</b>
        <span className="tag src">{STATUS[theme.status]}</span>
        <span className="hint">
          id {theme.slug} · from {BUILTIN_THEMES.find((b) => b.id === theme.basedOn)?.name ?? theme.basedOn}
        </span>
      </div>
      <Button
        size="sm"
        onClick={() => {
          onEdit(editing ? null : theme.id);
        }}
      >
        {editing ? 'Close' : 'Edit'}
      </Button>
      {theme.status === 'draft' && (
        <Button
          size="sm"
          variant="danger"
          onClick={() => {
            del.mutate(theme.id, {
              onSuccess: () => {
                toast(`Deleted the draft ${theme.name}`);
              },
              onError: fail,
            });
          }}
        >
          Delete
        </Button>
      )}
      {theme.status === 'approved' && (
        <Button
          size="sm"
          onClick={() => {
            set('retired', `Retired ${theme.name}; its users go back to the default theme`);
          }}
        >
          Retire
        </Button>
      )}
      {theme.status === 'retired' && (
        <Button
          size="sm"
          onClick={() => {
            set('draft', `${theme.name} is a draft again`);
          }}
        >
          Back to draft
        </Button>
      )}
    </li>
  );
}

/** The preview: a small piece of the app drawn with the edited colours (scoped to this element). */
function Preview({ colours, mode }: { colours: Record<string, string>; mode: Mode }) {
  const style: Record<string, string> = {};
  for (const k of THEME_KEYS) if (colours[k]) style[`--${k}`] = colours[k];
  return (
    <div className="adm-preview" style={style} aria-label={`${mode} preview`}>
      <div className="adm-pv-bar">
        <span className="logo" aria-hidden="true" />
        <b>Finuance</b>
        <span className="adm-pv-chip">Payee contains kino</span>
      </div>
      <div className="adm-pv-row">
        <span>12.03.2025</span>
        <span>Kino pod Gwiazdami</span>
        <span className="adm-pv-neg">-60,58</span>
      </div>
      <div className="adm-pv-row sel">
        <span>15.03.2025</span>
        <span>Salary</span>
        <span className="adm-pv-pos">+9 580,00</span>
      </div>
      <div className="adm-pv-foot">
        <span className="adm-pv-muted">917 of 917 rows</span>
        <span className="adm-pv-link">Show in table</span>
        <span className="adm-pv-btn">Apply</span>
      </div>
    </div>
  );
}

function ThemeEditor({ theme }: { theme: AdminTheme }) {
  const update = useUpdateTheme();
  const status = useSetThemeStatus();
  const [name, setName] = useState(theme.name);
  const [tokens, setTokens] = useState<ThemeTokens>(() => structuredClone(theme.tokens));
  const [dirty, setDirty] = useState(false);
  const report = contrastReport(tokens);
  const failing = report.filter((r) => !r.ok);
  const valid = completeTokens(tokens);
  const set = (mode: Mode, key: string, value: string) => {
    setTokens({ ...tokens, [mode]: { ...tokens[mode], [key]: value.trim().toLowerCase() } });
    setDirty(true);
  };
  const fail = (e: unknown) => {
    if (e instanceof ApiError && e.problem.contrast?.length) {
      toast(`${String(e.problem.contrast.length)} colour pairs are too hard to read; see the report below.`, {
        kind: 'error',
      });
      return;
    }
    toast(errorMessage(e), { kind: 'error' });
  };
  const save = () => {
    update.mutate(
      { id: theme.id, body: { version: theme.version, name: name.trim(), tokens } },
      {
        onSuccess: () => {
          toast(`Saved ${name}`);
        },
        onError: fail,
      },
    );
  };
  return (
    <div className="set-card">
      <div className="lbl">Edit {theme.name}</div>
      <label className="st-col">
        Name
        <TextInput
          value={name}
          maxLength={40}
          onChange={(e) => {
            setName(e.target.value);
            setDirty(true);
          }}
        />
      </label>
      <div className="adm-editor">
        {(['dark', 'light'] as const).map((mode) => (
          <div key={mode} className="adm-variant">
            <div className="lbl sub">{mode === 'dark' ? 'Dark variant' : 'Light variant'}</div>
            <Preview colours={tokens[mode]} mode={mode} />
            {THEME_KEYS.map((k) => {
              const v = tokens[mode][k] ?? '';
              const ok = HEX_COLOUR.test(v);
              return (
                <div key={k} className="adm-colour">
                  <input
                    type="color"
                    aria-label={`${KEY_LABEL[k] ?? k}, ${mode}, picker`}
                    value={ok ? v.slice(0, 7) : '#000000'}
                    onChange={(e) => {
                      set(mode, k, e.target.value + (v.length === 9 ? v.slice(7) : ''));
                    }}
                  />
                  <label>
                    <span>{KEY_LABEL[k] ?? k}</span>
                    <TextInput
                      mono
                      aria-label={`${KEY_LABEL[k] ?? k}, ${mode}`}
                      aria-invalid={!ok}
                      value={v}
                      onChange={(e) => {
                        set(mode, k, e.target.value);
                      }}
                    />
                  </label>
                </div>
              );
            })}
          </div>
        ))}
      </div>
      <div className="lbl sub">Readability</div>
      {failing.length === 0 ? (
        <p className="hint" role="status">
          Every text colour reaches 4.5:1 on its surfaces in both variants.
        </p>
      ) : (
        <table className="adm-table" aria-label="Colour pairs below 4.5:1">
          <thead>
            <tr>
              <th scope="col">Variant</th>
              <th scope="col">Text</th>
              <th scope="col">On</th>
              <th scope="col" className="num">
                Contrast
              </th>
            </tr>
          </thead>
          <tbody>
            {failing.map((r) => (
              <tr key={`${r.mode}:${r.fg}:${r.bg}`}>
                <td>{r.mode}</td>
                <td>{KEY_LABEL[r.fg] ?? r.fg}</td>
                <td>
                  {r.bg
                    .split('/')
                    .map((k) => KEY_LABEL[k] ?? k)
                    .join(' over ')}
                </td>
                <td className="num">{r.ratio.toFixed(2)}:1</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <div className="set-actions">
        <Button size="sm" disabled={!dirty || !valid || update.isPending} onClick={save}>
          Save
        </Button>
        {theme.status !== 'approved' && (
          <Button
            size="sm"
            variant="primary"
            disabled={dirty || failing.length > 0 || !valid || status.isPending}
            title={dirty ? 'Save first' : failing.length > 0 ? 'Fix the colour pairs below 4.5:1 first' : undefined}
            onClick={() => {
              status.mutate(
                { id: theme.id, body: { version: theme.version, status: 'approved' } },
                {
                  onSuccess: () => {
                    toast(`Approved ${theme.name}; everyone can pick it now`);
                  },
                  onError: fail,
                },
              );
            }}
          >
            Approve
          </Button>
        )}
      </div>
    </div>
  );
}
