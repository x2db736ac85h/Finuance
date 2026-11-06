# Administration

An instance can have administrators: people who manage accounts, limits, where a new user's view
starts, which parts of the app exist, and colour themes. Administrators see accounts and counts,
never anyone's transactions, imports or chats (ADR 0025).

## The first administrator

Create it with the CLI on the server, the way the first account was always made. The password is
read from stdin, so it never appears in the shell history:

```bash
printf '%s\n' 'a long password' | finuance admin create-user -email=you@example.com -admin -password-stdin
```

Or make an existing account an administrator, and take the role away again:

```bash
finuance admin grant-admin -email=you@example.com
```

```bash
finuance admin revoke-admin -email=you@example.com
```

On Kubernetes run them in the api pod (`kubectl -n finuance exec -i deploy/finuance-api -- /finuance admin …`),
with Compose `docker compose exec -T api /finuance admin …`. The last active administrator can't be
demoted, disabled or deleted.

## The panel

Administrators find **Administration** in the account menu (the initial in the top-right corner)
and at the end of the Settings list. Everyone else gets `404` from its routes.

### Users

- **Create account**: e-mail, display name and either a password you set or none, in which case a
  **one-time password** is generated and shown once. At the first sign-in with it the person must
  choose their own password before anything else works.
- **Make administrator / Remove administrator**.
- **Disable account**: signs it out everywhere; signing in shows "This account is disabled".
  **Enable account** undoes it.
- **Reset password**: a new one-time password, and the account is signed out everywhere.
- **Sign out everywhere**.
- **Limits**: per account; an empty field uses the instance default.
- **Delete account**: everything the person has goes with it; type the e-mail address to confirm.

Each account shows counts only: accounts, transactions, imports, stored file size, chats and the
last recorded activity.

Every action appears in your Settings → Activity (with the account's e-mail) and in the
person's own Activity as "An administrator …", without your name.

### Defaults and features

One document for the whole instance; saving it while another administrator changed it asks you to
reload (`409`).

- **Where a new user's view starts**: colour theme, mode, the charts above the table and which of
  them are shown, the sidebar. These lie under each person's own preferences, so they apply to
  everything a person hasn't changed yet; their own choices always win.
- **Features**: switching Overview or the Assistant off hides it for everyone and its API answers
  `404`. Switching **Assistant digests** off (or the Assistant) stops every digest and hides the
  setting (ADR 0033). Switching **Friends** off hides Settings → Friends and its routes (ADR 0027); switching
  **Shared groups** off hides the Shared tab, *Share…* and their routes ([sharing.md](sharing.md)).
- **Default limits** for every account without its own.

### Limits

| Limit | Meaning | Outer bound |
|---|---|---|
| Accounts | at most this many accounts | — |
| Groups | at most this many groups | — |
| Shared groups | at most this many shared groups owned (archived ones don't count) | — |
| Imports | uploads in the last 24 hours | — |
| Largest file | the biggest CSV upload | `FINUANCE_MAX_UPLOAD_BYTES` |
| Assistant questions | per hour | `FINUANCE_AI_RATE_PER_HOUR` |
| Assistant tokens | per UTC day, questions and digests together | `FINUANCE_AI_TOKENS_PER_DAY` (200,000) |

An account's own limit wins over the instance default, and both are capped by the server's
environment, so an administrator can only lower those two. A request over a limit gets
`422 limit_reached`, which the app shows as "Your account allows … Ask an administrator if you need more."

### Colour themes

Users choose from four built-in themes (Indigo, Fjord, Grove, Ember), each in a dark and a light
variant, plus any theme an administrator approved. To make one:

1. **New theme**: start from a built-in theme and give it a name; it is a draft.
2. **Edit**: change any of the 16 colours of both variants, with a live preview of each. The
   readability report lists every text colour below 4.5:1 on its surface.
3. **Approve** once the report is empty (the server checks again). Everyone can pick it from then on.
4. **Retire** an approved theme; its users go back to the instance default. Drafts can be deleted.

Themes are colours only (`#rrggbb` or `#rrggbbaa`), never CSS (ADR 0026). The chart colours stay
the same in every theme.

### Assistant

- **Connections**: the model servers people pick from (ADR 0035). The configuration file's are
  listed read only; *Add a connection* stores another one (name, label, API, address, cloud or
  not, default model, the models offered, a context size, an API key). The key is stored sealed
  under `FINUANCE_INSTANCE_KEY` and never shown again ("stored", "none", or "unreadable" when it
  was sealed under another instance key); changing the address drops it, so enter it again. Other
  api replicas and the worker pick a change up within a minute.
- **Tuning per model**: for one model, the temperature, thinking level and tool set used when a
  question doesn't set them, the context size, and a short note for that model.
- **Instructions for the assistant**: up to 2,000 characters added to the instructions of every
  question and digest on the instance, after the app's own rules (which they can't change). Every
  user's model reads them, so keep them to tone, language and pointers to articles.
- **Advice library**: short markdown articles the assistant searches for general money questions
  (ADR 0034). Each has a title, an address (slug, unique per language), a two-letter language,
  tags (searched like the title) and a status: only **published** articles are found and readable.
  *Add the starter articles* adds eight original English articles (emergency fund, splitting income,
  subscriptions, debt, saving for a goal, irregular expenses, spending by category, sharing costs)
  as drafts: read them, adjust them to your instance and publish the ones you want. Each ends with a
  "general information, not financial advice" note. Saving, publishing and deleting are in your
  Activity.

### Usage

Instance totals (accounts, active people, transactions, imports, stored files, the assistant's
tokens) and the last 30 days of imports, assistant questions and tokens. A user's detail shows
their assistant runs and tokens over the last 30 days, never what they asked.

### Developer

Off unless the instance turns on **developer mode**. Then administrators get a *Developer* section
with:

- **Build**: version (linked to its release), commit (linked to it), build time, Go version,
  environment, the api replica that answered, and the database schema version (marked when it is
  behind what the build expects).
- **Source**: the repository.
- **Links** to the tools around the instance (dashboards, Argo CD, CI/CD), grouped, each opening in
  a new tab. The **ⓘ** next to a link shows how to get in on hover or keyboard focus (a tap keeps it
  open), with a **Copy** button for each command, such as the port-forward or the one that prints a
  password.

Turn it on:

| Where | How |
|---|---|
| Kubernetes (Helm chart) | `developer.enabled: true`, `developer.source`, `developer.links` (see the chart's `values.yaml`; the homelab's are in `deploy/environments/homelab/values.yaml`) |
| Compose, local process | `FINUANCE_DEVELOPER_MODE=true`, optionally `FINUANCE_DEVELOPER_LINKS_FILE` (example: `deploy/developer/links.example.json`; `make up` mounts the folder at `/etc/finuance/developer`) |
| `compose.prod.yaml` | `FINUANCE_DEVELOPER_MODE=true` in `.env` (the build only) |

The links file is JSON: `source` and up to 40 `links` with `group`, `name`, `url`, `note` and up
to 4 `commands`. The api refuses to start on a bad file and names every problem.

Who can see it, and what it may hold:

- Only administrators. `GET /api/v1/admin/developer` answers `404` to everyone else, and to
  administrators too while the mode is off, so nobody else can tell whether it is on. `/meta` and
  `/me` don't mention it, the web app holds none of the links (they come from that request only),
  and the response is never cached.
- **No secrets.** Notes say *how* to get a credential (a command that needs cluster access, a
  password manager), never the credential. Links must be `http`/`https` and may not carry a
  `user:password`; text is shown as plain text.
- Never on a public demo: `FINUANCE_DEVELOPER_MODE` together with `FINUANCE_DEMO_MODE` is a
  configuration error.

## API

Everything the panel does is in `api/openapi.yaml` under the `admin` tag (`/api/v1/admin/…`).
