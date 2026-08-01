{{- define "hby.name" -}}
{{- default .root.Chart.Name .hby.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "hby.fullname" -}}
{{- if .hby.fullnameOverride -}}
{{- .hby.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "hby.name" . -}}
{{- if contains $name .root.Release.Name -}}{{ .root.Release.Name | trunc 63 | trimSuffix "-" }}{{- else -}}{{ printf "%s-%s" .root.Release.Name $name | trunc 63 | trimSuffix "-" }}{{- end -}}
{{- end -}}
{{- end -}}

{{- define "hby.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hby.name" . }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end -}}

{{- define "hby.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .root.Chart.Name .root.Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "hby.selectorLabels" . }}
app.kubernetes.io/version: {{ .root.Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
{{- end -}}

{{- define "hby.serviceAccountName" -}}
{{- if .hby.serviceAccount.create -}}{{ default (include "hby.fullname" .) .hby.serviceAccount.name }}{{- else -}}{{ default "default" .hby.serviceAccount.name }}{{- end -}}
{{- end -}}

{{- define "hby.pvcName" -}}
{{- default (printf "%s-data" (include "hby.fullname" .)) .hby.persistence.existingClaim | trunc 63 | trimSuffix "-" -}}
{{- end -}}
