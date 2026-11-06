# The `fin` command line

`fin` talks to a Finuance instance from a terminal or a script, with a **personal access token**
(ADR 0036). Its client is generated from `api/openapi.yaml` (`make gen`), so it follows the API.

## Install

Download `fin_<version>_<os>_<arch>` for Linux, macOS or Windows (amd64 or arm64) from the release,
check it against `fin_<version>_checksums.txt` (`sha256sum -c --ignore-missing`), and put `fin` on
your `PATH`. From a checkout: `make fin` builds `backend/bin/fin`.

## Sign in

1. In the app, **Settings → Access tokens**: make a token. *Read only* looks things up; *read and
   change data* also imports and asks the assistant. No token can sign in, change the password,
   manage sessions or tokens, or reach Administration. Copy it: it is shown once.
2. `fin login --url https://finance.example.com` and paste the token (it is read from stdin, so it
   doesn't end up in your shell's history). It is saved in `~/.config/fin/config.json` (on macOS
   `~/Library/Application Support/fin/`, on Windows `%AppData%\fin\`), readable by you only.

`FIN_URL` and `FIN_TOKEN`, or `--url` and `--token`, override the file (for CI jobs and scripts).
A new password, an administrator's reset and *Sign out everywhere* end every token.

## Commands

| Command | What it does |
|---|---|
| `fin whoami` | the account the token belongs to |
| `fin accounts` | accounts with their transactions and date range |
| `fin transactions` | transactions, newest first: `--account NAME`, `--search WORDS`, `--category`, `--payee`, `--month YYYY-MM` or `--from`/`--to` (inclusive), `--limit N` (50; at most 1,000) |
| `fin transactions --export FILE` | the same filters as the app's CSV export (`-` for stdout) |
| `fin import FILE [--account NAME] [--commit]` | uploads a bank CSV and shows the preview; `--commit` imports it and waits for the job |
| `fin overview [--month YYYY-MM] [--account NAME]` | one month: income, spending, net and each category against its budget |
| `fin groups`, `fin shared`, `fin budgets`, `fin goals` | your groups, shared groups, budgets and goals |
| `fin ask "QUESTION"` | asks the assistant in a new chat; the answer streams to stdout, its lookups to stderr. Proposals wait in the chat in the app |

`--output table` (default), `csv` or `json` (the API's own answer) before the command:

```sh
fin --output csv transactions --month 2025-03 --category groceries > groceries.csv
fin --output json overview | jq '.data.current'
fin import ~/Downloads/statement.csv --account "Main account" --commit
fin ask "How much did I spend on eating out last month?"
```

Exit codes: 0 done, 1 the server refused or something failed (the API's message on stderr), 2 a
wrong command or flag.
