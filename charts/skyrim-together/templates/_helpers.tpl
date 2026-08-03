{{/*
Expand the name of the chart.
*/}}
{{- define "skyrim-together.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "skyrim-together.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "skyrim-together.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "skyrim-together.labels" -}}
helm.sh/chart: {{ include "skyrim-together.chart" . }}
{{ include "skyrim-together.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "skyrim-together.selectorLabels" -}}
app.kubernetes.io/name: {{ include "skyrim-together.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use.
*/}}
{{- define "skyrim-together.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "skyrim-together.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "skyrim-together.pvcName" -}}
{{- default (printf "%s-data" (include "skyrim-together.fullname" .)) .Values.persistence.existingClaim | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "skyrim-together.externalSecretName" -}}
{{- $root := .root -}}
{{- $metadata := default dict .metadata -}}
{{- default (printf "%s-secret" (include "skyrim-together.fullname" $root)) $metadata.name -}}
{{- end -}}

{{- define "skyrim-together.externalSecretTargetName" -}}
{{- $root := .root -}}
{{- $metadata := default dict .metadata -}}
{{- $spec := default dict .spec -}}
{{- $target := default dict $spec.target -}}
{{- $name := include "skyrim-together.externalSecretName" (dict "root" $root "metadata" $metadata) -}}
{{- default $name $target.name -}}
{{- end -}}
