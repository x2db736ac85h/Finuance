{{- define "db.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: finuance
{{- end -}}

{{- define "db.store" -}}{{ .Values.cluster.name }}-store{{- end -}}
{{- define "db.plugin" -}}barman-cloud.cloudnative-pg.io{{- end -}}

{{- define "db.objectStoreUsed" -}}
{{- if or .Values.backup.enabled .Values.recovery.enabled -}}true{{- end -}}
{{- end -}}
