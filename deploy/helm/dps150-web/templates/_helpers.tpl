{{/*
Expand the name of the chart.
*/}}
{{- define "dps150-web.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "dps150-web.fullname" -}}
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
{{- define "dps150-web.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "dps150-web.labels" -}}
helm.sh/chart: {{ include "dps150-web.chart" . }}
{{ include "dps150-web.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "dps150-web.selectorLabels" -}}
app.kubernetes.io/name: {{ include "dps150-web.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Backend resource name
*/}}
{{- define "dps150-web.backend.fullname" -}}
{{- printf "%s-backend" (include "dps150-web.fullname" .) }}
{{- end }}

{{/*
Frontend resource name
*/}}
{{- define "dps150-web.frontend.fullname" -}}
{{- printf "%s-frontend" (include "dps150-web.fullname" .) }}
{{- end }}

{{/*
Registry pull-secret name (materialised from Vault by VSO)
*/}}
{{- define "dps150-web.registryCredsName" -}}
{{- printf "%s-registry-creds" (include "dps150-web.fullname" .) }}
{{- end }}

{{/*
imagePullSecrets: the private GitLab registry needs credentials; they are
synced from Vault when the Vault Secrets Operator integration is enabled.
*/}}
{{- define "dps150-web.imagePullSecrets" -}}
{{- if .Values.vaultSecretsOperator.enabled }}
imagePullSecrets:
  - name: {{ include "dps150-web.registryCredsName" . }}
{{- end }}
{{- end }}
