{{/* Names and labels. */}}
{{- define "finuance.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "finuance.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ include "finuance.tag" . | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: finuance
{{- end -}}

{{/* Selector labels of one component: api, worker, web or migrate. */}}
{{- define "finuance.selector" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "finuance.tag" -}}
{{- .Values.image.tag | default .Chart.AppVersion -}}
{{- end -}}

{{/*
The image of one component ("api" or "web"). With image.digests set it is registry/name@sha256:…
and the tag only labels the release (PLAN 23.1); both digests or neither.
*/}}
{{- define "finuance.image" -}}
{{- $digests := .root.Values.image.digests | default dict -}}
{{- if ne (empty $digests.api) (empty $digests.web) -}}
{{- fail "image.digests needs both api and web (or neither, to deploy by tag)" -}}
{{- end -}}
{{- $digest := get $digests .component | default "" -}}
{{- if $digest -}}
{{- printf "%s/%s@%s" .root.Values.image.registry (get .root.Values.image .component) $digest -}}
{{- else -}}
{{- printf "%s/%s:%s" .root.Values.image.registry (get .root.Values.image .component) (include "finuance.tag" .root) -}}
{{- end -}}
{{- end -}}

{{/* A password-less database URL; the password comes from PGPASSWORD (finuance.dbPassword). */}}
{{- define "finuance.dbURL" -}}
{{- $db := .root.Values.database -}}
{{- printf "postgres://%s@%s:%v/%s?sslmode=%s" .user $db.host $db.port $db.name $db.sslmode -}}
{{- end -}}

{{- define "finuance.dbPassword" -}}
- name: PGPASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .passwordSecret }}
      key: {{ .passwordKey }}
{{- end -}}

{{/* Pod and container hardening (PLAN 16.1): non-root, read-only root, no capabilities. */}}
{{- define "finuance.podSecurity" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .uid }}
fsGroup: {{ .uid }}
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "finuance.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: [ALL]
{{- end -}}
