#!/usr/bin/env bash
# The CloudNativePG cluster runs its own copy of the database bootstrap (deploy/k8s/db/files/bootstrap.sql):
# CNPG creates the database and the owner itself, and the managed roles set the passwords. This fails
# when that copy and deploy/db/bootstrap.sql differ in anything else: the same statements, in the
# same order, once comments, whitespace, the CREATE ROLE / CREATE DATABASE lines and psql's
# \connect are left out (the k8s copy's one extra statement is the password-less app role).
set -euo pipefail
cd "$(dirname "$0")/.."
python3 -I - deploy/db/bootstrap.sql deploy/k8s/db/files/bootstrap.sql <<'PY'
import re, sys

def statements(path):
    lines = []
    for line in open(path, encoding="utf-8"):
        line = re.sub(r"--.*", "", line).strip()
        if line and not line.startswith("\\"):
            lines.append(line)
    text = " ".join(lines)
    return [re.sub(r"\s+", " ", s).strip() for s in text.split(";") if s.strip()]

compose, k8s = (statements(p) for p in sys.argv[1:3])
want = [s for s in compose if not re.match(r"CREATE (ROLE|DATABASE) ", s)]
extra = "CREATE ROLE finuance_app LOGIN"
got = [s for s in k8s if s != extra]
problems = []
if extra not in k8s:
    problems.append(f"the k8s copy must start the app role with: {extra};")
if [s for s in k8s if re.match(r"CREATE (ROLE|DATABASE) ", s) and s != extra]:
    problems.append("the k8s copy must not create other roles or databases (CNPG and the managed roles do)")
if got != want:
    for i in range(max(len(got), len(want))):
        g = got[i] if i < len(got) else "(missing)"
        w = want[i] if i < len(want) else "(missing)"
        if g != w:
            problems.append(f"statement {i + 1}:\n  deploy/db:  {w}\n  k8s copy:   {g}")
            break
if problems:
    print("check-db-bootstrap: deploy/k8s/db/files/bootstrap.sql differs from deploy/db/bootstrap.sql")
    print("\n".join(problems))
    sys.exit(1)
print(f"check-db-bootstrap: ok ({len(want)} statements)")
PY
