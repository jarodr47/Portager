{{/*
Expand the name of the chart.
*/}}
{{- define "portager.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "portager.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "portager.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "portager.labels" -}}
helm.sh/chart: {{ include "portager.chart" . }}
{{ include "portager.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "portager.selectorLabels" -}}
app.kubernetes.io/name: {{ include "portager.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Create the name of the service account to use.
*/}}
{{- define "portager.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "portager.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Container image reference.
*/}}
{{- define "portager.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Fail fast on an ambiguous or incomplete caBundle configuration. Rendering a
Deployment that silently ignores a misconfigured CA is far worse than refusing
to render, because the failure would otherwise surface as an x509 error at the
first sync, long after install.
*/}}
{{- define "portager.caBundle.validate" -}}
{{- if .Values.caBundle.enabled -}}
{{- $sources := list -}}
{{- if .Values.caBundle.existingSecret }}{{- $sources = append $sources "existingSecret" -}}{{- end -}}
{{- if .Values.caBundle.existingConfigMap }}{{- $sources = append $sources "existingConfigMap" -}}{{- end -}}
{{- if .Values.caBundle.inline }}{{- $sources = append $sources "inline" -}}{{- end -}}
{{- if eq (len $sources) 0 -}}
{{- fail "caBundle.enabled is true but no source is set. Set exactly one of caBundle.existingSecret, caBundle.existingConfigMap, or caBundle.inline." -}}
{{- end -}}
{{- if gt (len $sources) 1 -}}
{{- fail (printf "caBundle accepts exactly one source, but %d were set: %s" (len $sources) (join ", " $sources)) -}}
{{- end -}}
{{- if not (has .Values.caBundle.mode (list "append" "replace")) -}}
{{- fail (printf "caBundle.mode must be \"append\" or \"replace\", got %q" (toString .Values.caBundle.mode)) -}}
{{- end -}}
{{- if not .Values.caBundle.key -}}
{{- fail "caBundle.key must not be empty." -}}
{{- end -}}
{{- end -}}
{{- end }}

{{/*
The CA bundle volume. Only the configured key is projected, so unrelated keys in
the source object (a tls.key alongside tls.crt, for instance) never land in the
certificate directory where Go would try to parse them.
*/}}
{{- define "portager.caBundle.volume" -}}
- name: ca-bundle
  {{- if .Values.caBundle.existingSecret }}
  secret:
    secretName: {{ .Values.caBundle.existingSecret }}
    defaultMode: 0444
    items:
      - key: {{ .Values.caBundle.key }}
        path: {{ .Values.caBundle.key }}
  {{- else }}
  configMap:
    name: {{ if .Values.caBundle.existingConfigMap }}{{ .Values.caBundle.existingConfigMap }}{{ else }}{{ include "portager.fullname" . }}-ca-bundle{{ end }}
    defaultMode: 0444
    items:
      - key: {{ .Values.caBundle.key }}
        path: {{ .Values.caBundle.key }}
  {{- end }}
{{- end }}
