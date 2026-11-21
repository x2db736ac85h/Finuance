{{- define "telemetry.labels" -}}
app.kubernetes.io/part-of: finuance-telemetry
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "telemetry.dnsEgress" -}}
- to:
    - namespaceSelector: {{- toYaml .Values.dns.namespaceSelector | nindent 8 }}
      podSelector: {{- toYaml .Values.dns.podSelector | nindent 8 }}
  ports:
    - { port: 53, protocol: UDP }
    - { port: 53, protocol: TCP }
{{- end -}}

{{- define "telemetry.podSecurity" -}}
runAsNonRoot: true
runAsUser: {{ .uid }}
runAsGroup: {{ .uid }}
fsGroup: {{ .uid }}
seccompProfile: { type: RuntimeDefault }
{{- end -}}

{{- define "telemetry.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities: { drop: [ALL] }
{{- end -}}


{{- define "telemetry.lokiConfig" -}}
auth_enabled: false
server:
  http_listen_port: 3100
  grpc_listen_port: 9095
  log_level: info
common:
  instance_addr: 127.0.0.1
  path_prefix: /var/loki
  storage:
    filesystem:
      chunks_directory: /var/loki/chunks
      rules_directory: /var/loki/rules
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory
schema_config:
  configs:
    - from: "2026-11-01"
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h
compactor:
  working_directory: /var/loki/compactor
  retention_enabled: true
  delete_request_store: filesystem
limits_config:
  retention_period: {{ .Values.logs.retention }}
  ingestion_rate_mb: {{ .Values.logs.ingestion.rateMB }}
  ingestion_burst_size_mb: {{ .Values.logs.ingestion.burstMB }}
  reject_old_samples: true
  reject_old_samples_max_age: 168h
query_range:
  results_cache:
    cache:
      embedded_cache:
        enabled: true
        max_size_mb: 50
# no log patterns, no rules: nothing reads them here
pattern_ingester:
  enabled: false
ruler:
  enable_api: false
analytics:
  reporting_enabled: false
{{- end -}}
