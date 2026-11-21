{{- define "sim.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "sim.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
app.kubernetes.io/name: finuance-sim
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: finuance
{{- end -}}

{{- define "sim.selector" -}}
app.kubernetes.io/name: finuance-sim
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/* The image of one component ("sim" or "mock"): by digest when image.digests has one. */}}
{{- define "sim.image" -}}
{{- $digest := get (.root.Values.image.digests | default dict) .component | default "" -}}
{{- if $digest -}}
{{- printf "%s/%s@%s" .root.Values.image.registry (get .root.Values.image .component) $digest -}}
{{- else -}}
{{- printf "%s/%s:%s" .root.Values.image.registry (get .root.Values.image .component) (.root.Values.image.tag | default .root.Chart.AppVersion) -}}
{{- end -}}
{{- end -}}

{{/* restricted Pod Security: non-root, no privilege escalation, no capabilities, read-only root */}}
{{- define "sim.podSecurity" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{- define "sim.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: [ALL]
{{- end -}}

{{- define "sim.dnsEgress" -}}
{{- $dns := .Values.networkPolicy.dns -}}
- to:
    - namespaceSelector: {{- toYaml $dns.namespaceSelector | nindent 8 }}
      podSelector: {{- toYaml $dns.podSelector | nindent 8 }}
  ports:
    - { port: 53, protocol: UDP }
    - { port: 53, protocol: TCP }
{{- end -}}

{{/* Egress entries: ports and either a cidr (outside the cluster) or peers in `to`. */}}
{{- define "sim.egress" -}}
{{- range . }}
- to:
    {{- if .cidr }}
    - ipBlock: { cidr: {{ .cidr }} }
    {{- else }}
    {{- toYaml .to | nindent 4 }}
    {{- end }}
  ports:
    {{- range .ports }}
    - { port: {{ . }}, protocol: TCP }
    {{- end }}
{{- end }}
{{- end -}}
