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

Administrators find **Administration** in the account menu (the initial in the top-right corner).
Everyone else gets `404` from its routes.

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
  `404`. (Friends and Shared groups arrive later in Phase 19.)
- **Default limits** for every account without its own.

### Limits

| Limit | Meaning | Outer bound |
|---|---|---|
| Accounts | at most this many accounts | — |
| Groups | at most this many groups | — |
| Imports | uploads in the last 24 hours | — |
| Largest file | the biggest CSV upload | `FINUANCE_MAX_UPLOAD_BYTES` |
| Assistant questions | per hour | `FINUANCE_AI_RATE_PER_HOUR` |

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

### Usage

Instance totals (accounts, active people, transactions, imports, stored files) and the last 30
days of imports and assistant questions.

## API

Everything the panel does is in `api/openapi.yaml` under the `admin` tag (`/api/v1/admin/…`).
