# RustFS keys (homelab)

`rustfs.sealed.yaml`: the SealedSecret `s3/rustfs` with the in-cluster backup store's keys
(`RUSTFS_ACCESS_KEY`, `RUSTFS_SECRET_KEY`), read by the `rustfs` Argo CD application. It is made
together with `finuance-db-backup` (same keys, namespace `finuance`) by

    tools/seal-secrets.sh homelab backup-store

which generates a random key pair in memory and seals it twice (docs/RUNBOOK.md, 18.3).
