{{/*
Expand the name of the chart.
*/}}
{{- define "mariadb-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "mariadb-operator.fullname" -}}
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
{{- define "mariadb-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "mariadb-operator.labels" -}}
helm.sh/chart: {{ include "mariadb-operator.chart" . }}
{{ include "mariadb-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "mariadb-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mariadb-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Webhook common labels
*/}}
{{- define "mariadb-operator-webhook.labels" -}}
helm.sh/chart: {{ include "mariadb-operator.chart" . }}
{{ include "mariadb-operator-webhook.selectorLabels" . }}
{{ if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{ end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Webhook selector labels
*/}}
{{- define "mariadb-operator-webhook.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mariadb-operator.name" . }}-webhook
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Webhook certificate subject name
*/}}
{{- define "mariadb-operator-webhook.subjectName" -}}
{{- include "mariadb-operator.fullname" . }}-webhook.{{ .Release.Namespace }}.svc
{{- end }}

{{/*
Webhook certificate subject alternative name
*/}}
{{- define "mariadb-operator-webhook.altName" -}}
{{- include "mariadb-operator.fullname" . }}-webhook.{{ .Release.Namespace }}.svc.{{ .Values.clusterName }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "mariadb-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.enabled }}
{{- default (include "mariadb-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the webhook service account to use
*/}}
{{- define "mariadb-operator-webhook.serviceAccountName" -}}
{{- if .Values.webhook.serviceAccount.enabled }}
{{- default (printf "%s-webhook" (include "mariadb-operator.fullname" .))  .Values.webhook.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.webhook.serviceAccount.name }}
{{- end }}
{{- end }}
