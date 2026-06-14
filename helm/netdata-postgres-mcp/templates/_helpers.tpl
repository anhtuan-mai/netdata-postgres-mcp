{{/*
Expand the name of the chart.
*/}}
{{- define "netdata-postgres-mcp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this
(by the DNS naming spec). If release name contains chart name it will be used as
a full name.
*/}}
{{- define "netdata-postgres-mcp.fullname" -}}
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
{{- define "netdata-postgres-mcp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "netdata-postgres-mcp.labels" -}}
helm.sh/chart: {{ include "netdata-postgres-mcp.chart" . }}
{{ include "netdata-postgres-mcp.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "netdata-postgres-mcp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "netdata-postgres-mcp.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Return the secret name to use (either existing or chart-managed).
*/}}
{{- define "netdata-postgres-mcp.secretName" -}}
{{- if .Values.existingSecret }}
{{- .Values.existingSecret }}
{{- else }}
{{- include "netdata-postgres-mcp.fullname" . }}
{{- end }}
{{- end }}

{{/*
Return the configmap name.
*/}}
{{- define "netdata-postgres-mcp.configmapName" -}}
{{- include "netdata-postgres-mcp.fullname" . }}
{{- end }}
