# Monitoring secrets (homelab)

`finuance-alert-smtp.sealed.yaml`: the SealedSecret `monitoring/finuance-alert-smtp` (key `password`)
with the app password of the Gmail account that sends Finuance's alert e-mails (Phase 18.6; read by
the AlertmanagerConfig of `deploy/k8s/observability`). Made with

    tools/seal-secrets.sh homelab smtp

which asks for the password without showing it. Revoke it in the sending account under
*Google Account → Security → App passwords* if the cluster or the controller key is ever exposed.
The addresses are not here: they are in the private environment repository's overlay.

`grafana-admin.sealed.yaml`: the SealedSecret `monitoring/grafana-admin` (keys `admin-user`,
`admin-password`) with Grafana's admin login, a random password (the chart's default is public).
Synced by the `kube-prometheus-stack` app, so Grafana can start before Finuance's own monitoring
app. Made with

    tools/seal-secrets.sh homelab grafana

and read back with `kubectl -n monitoring get secret grafana-admin -o jsonpath='{.data.admin-password}' | base64 -d`
(docs/RUNBOOK.md "Monitoring").
