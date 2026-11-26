{{- define "georef.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "georef.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "georef.selector" -}}
app.kubernetes.io/name: {{ .root.Chart.Name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "georef.claim" -}}
{{- .Values.storage.existingClaim | default (printf "%s-storage" (include "georef.fullname" .)) -}}
{{- end -}}

{{/* Kept by Helm on uninstall and never pruned or deleted by Argo CD: losing these loses data. */}}
{{- define "georef.keep" -}}
helm.sh/resource-policy: keep
argocd.argoproj.io/sync-options: Delete=false,Prune=false
{{- end -}}

{{/* DB_* for server and worker, from the CNPG app secret or an external one. */}}
{{- define "georef.dbEnv" -}}
{{- if .Values.database.cnpg.enabled }}
{{- $s := printf "%s-db-app" (include "georef.fullname" .) }}
- name: DB_HOST
  valueFrom: {secretKeyRef: {name: {{ $s }}, key: host}}
- name: DB_PORT
  valueFrom: {secretKeyRef: {name: {{ $s }}, key: port}}
- name: DB_NAME
  valueFrom: {secretKeyRef: {name: {{ $s }}, key: dbname}}
- name: DB_USER
  valueFrom: {secretKeyRef: {name: {{ $s }}, key: username}}
- name: DB_PASSWORD
  valueFrom: {secretKeyRef: {name: {{ $s }}, key: password}}
{{- else }}
{{- $e := .Values.database.external }}
- name: DB_HOST
  value: {{ required "database.external.host is required" $e.host | quote }}
- name: DB_PORT
  value: {{ $e.port | quote }}
- name: DB_NAME
  value: {{ $e.name | quote }}
- name: DB_USER
  valueFrom: {secretKeyRef: {name: {{ required "database.external.existingSecret is required" $e.existingSecret }}, key: {{ $e.userKey }}}}
- name: DB_PASSWORD
  valueFrom: {secretKeyRef: {name: {{ $e.existingSecret }}, key: {{ $e.passwordKey }}}}
{{- end }}
- name: DB_SSLMODE
  value: {{ .Values.database.sslmode | quote }}
- name: STORAGE_ROOT
  value: /data/storage
{{- end -}}

{{- define "georef.envMap" -}}
{{- range $k, $v := . }}
- name: {{ $k }}
  value: {{ $v | quote }}
{{- end }}
{{- end -}}
