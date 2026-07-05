{{/*
Expand the name of the chart.
*/}}
{{- define "minecraft-modrinth-fabric.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "minecraft-modrinth-fabric.fullname" -}}
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
{{- define "minecraft-modrinth-fabric.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "minecraft-modrinth-fabric.labels" -}}
helm.sh/chart: {{ include "minecraft-modrinth-fabric.chart" . }}
{{ include "minecraft-modrinth-fabric.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "minecraft-modrinth-fabric.selectorLabels" -}}
app.kubernetes.io/name: {{ include "minecraft-modrinth-fabric.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use.
*/}}
{{- define "minecraft-modrinth-fabric.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "minecraft-modrinth-fabric.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "minecraft-modrinth-fabric.installerConfigMapName" -}}
{{- printf "%s-installer" (include "minecraft-modrinth-fabric.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "minecraft-modrinth-fabric.pvcName" -}}
{{- default (printf "%s-data" (include "minecraft-modrinth-fabric.fullname" .)) .Values.persistence.existingClaim | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "minecraft-modrinth-fabric.tokenSecretName" -}}
{{- default (printf "%s-modrinth-token" (include "minecraft-modrinth-fabric.fullname" .)) .Values.modrinth.auth.existingSecret | trunc 63 | trimSuffix "-" -}}
{{- end -}}

