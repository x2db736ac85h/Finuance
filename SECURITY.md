# Security policy

Finuance stores people's financial history, so security reports are very welcome.

## Reporting a vulnerability

Please **do not open a public issue**. Report it privately through GitHub instead: on the repository's **Security** tab, choose **Report a vulnerability** ([private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)).

Include what you found, how to reproduce it, and what an attacker could do with it. You'll get an answer as soon as possible; a fix and an advisory follow once the issue is confirmed.

## Scope and status

Finuance is pre-1.0 software. Security fixes land on `main` and ship in the next release; only the latest release is supported, so please check that a problem still exists there. The protections in place today, the risks accepted on purpose, and what is still planned are listed in the [threat model](docs/THREAT-MODEL.md); the scans and their open findings are in [docs/security-scan.md](docs/security-scan.md).

Never include real bank data in a report. Synthetic data or a description of the file's layout is enough.
