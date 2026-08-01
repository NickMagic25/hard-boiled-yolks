{{- define "hby.serviceAccount" -}}
{{- if .hby.serviceAccount.create }}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "hby.serviceAccountName" . }}
  labels:
    {{- include "hby.labels" . | nindent 4 }}
  {{- with .hby.serviceAccount.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
automountServiceAccountToken: {{ .hby.serviceAccount.automount }}
{{- end }}
{{- end -}}

{{- define "hby.pvc" -}}
{{- if and .hby.persistence.enabled (not .hby.persistence.existingClaim) }}
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: {{ include "hby.pvcName" . }}
  labels:
    {{- include "hby.labels" . | nindent 4 }}
  {{- with .hby.persistence.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  accessModes:
    {{- toYaml .hby.persistence.accessModes | nindent 4 }}
  {{- with .hby.persistence.storageClassName }}
  storageClassName: {{ . | quote }}
  {{- end }}
  resources:
    requests:
      storage: {{ .hby.persistence.size | quote }}
{{- end }}
{{- end -}}

{{- define "hby.service" -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "hby.fullname" . }}
  labels:
    {{- include "hby.labels" . | nindent 4 }}
  {{- with .hby.service.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  type: {{ .hby.service.type }}
  ports:
    {{- range .hby.ports }}
    - name: {{ .name }}
      port: {{ .servicePort }}
      targetPort: {{ .name }}
      protocol: {{ .protocol }}
    {{- end }}
    {{- if and .hby.controller.enabled .hby.controller.service.enabled }}
    - name: control
      port: {{ .hby.controller.service.port }}
      targetPort: control
      protocol: TCP
    {{- end }}
  selector:
    {{- include "hby.selectorLabels" . | nindent 4 }}
{{- end -}}

{{- define "hby.deployment" -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "hby.fullname" . }}
  labels:
    {{- include "hby.labels" . | nindent 4 }}
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      {{- include "hby.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      {{- if or .hby.podAnnotations .podAnnotations }}
      annotations:
        {{- with .hby.podAnnotations }}{{ toYaml . | nindent 8 }}{{- end }}
        {{- with .podAnnotations }}{{ toYaml . | nindent 8 }}{{- end }}
      {{- end }}
      labels:
        {{- include "hby.labels" . | nindent 8 }}
        {{- with .hby.podLabels }}{{ toYaml . | nindent 8 }}{{- end }}
    spec:
      serviceAccountName: {{ include "hby.serviceAccountName" . }}
      {{- with .hby.imagePullSecrets }}
      imagePullSecrets:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      securityContext:
        {{- toYaml .hby.podSecurityContext | nindent 8 }}
      {{- with .initContainers }}
      initContainers:
        {{- . | nindent 8 }}
      {{- end }}
      containers:
        - name: {{ default "server" .containerName }}
          image: "{{ .hby.image.repository }}:{{ .hby.image.tag }}"
          imagePullPolicy: {{ .hby.image.pullPolicy }}
          workingDir: {{ .hby.persistence.mountPath | quote }}
          {{- with .hby.container.command }}
          command:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          {{- with .hby.container.args }}
          args:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          env:
            {{- with .env }}{{ . | nindent 12 }}{{- end }}
            {{- if .hby.controller.enabled }}
            - name: HBY_CONTROL_ENABLED
              value: "true"
            - name: HBY_CONTROL_ADDR
              value: {{ .hby.controller.address | quote }}
            - name: HBY_CONTROL_ROOT
              value: {{ .hby.controller.root | quote }}
            - name: HBY_CONTROL_SECURE_COOKIES
              value: {{ ternary "true" "false" .hby.controller.secureCookies | quote }}
            {{- else }}
            - name: HBY_CONTROL_ENABLED
              value: "false"
            {{- end }}
            {{- with .hby.controller.env }}{{ toYaml . | nindent 12 }}{{- end }}
            {{- with .hby.container.env }}{{ toYaml . | nindent 12 }}{{- end }}
          {{- with .hby.container.envFrom }}
          envFrom:
            {{- toYaml . | nindent 12 }}
          {{- end }}
          ports:
            {{- range .hby.ports }}
            - name: {{ .name }}
              containerPort: {{ .containerPort }}
              protocol: {{ .protocol }}
            {{- end }}
            {{- if .hby.controller.enabled }}
            - name: control
              containerPort: 8080
              protocol: TCP
            {{- end }}
          {{- if .hby.probes.enabled }}
          readinessProbe:
            tcpSocket:
              port: {{ default (ternary "control" "" .hby.controller.enabled) .hby.probes.port | required "hby.probes.port is required when controller is disabled" }}
            {{- toYaml .hby.probes.readiness | nindent 12 }}
          livenessProbe:
            tcpSocket:
              port: {{ default (ternary "control" "" .hby.controller.enabled) .hby.probes.port | required "hby.probes.port is required when controller is disabled" }}
            {{- toYaml .hby.probes.liveness | nindent 12 }}
          {{- end }}
          securityContext:
            {{- toYaml .hby.securityContext | nindent 12 }}
          resources:
            {{- toYaml .hby.resources | nindent 12 }}
          volumeMounts:
            - name: data
              mountPath: {{ .hby.persistence.mountPath | quote }}
            {{- with .hby.container.extraVolumeMounts }}{{ toYaml . | nindent 12 }}{{- end }}
      volumes:
        - name: data
          {{- if .hby.persistence.enabled }}
          persistentVolumeClaim:
            claimName: {{ include "hby.pvcName" . }}
          {{- else }}
          emptyDir: {}
          {{- end }}
        {{- with .hby.extraVolumes }}{{ toYaml . | nindent 8 }}{{- end }}
        {{- with .extraVolumes }}{{ . | nindent 8 }}{{- end }}
      {{- with .hby.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .hby.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .hby.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
{{- end -}}

{{- define "hby.externalSecrets" -}}
{{- if .hby.externalSecrets.enabled }}
{{- range $item := .hby.externalSecrets.items }}
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: {{ required "hby.externalSecrets.items[].name is required" $item.name }}
  namespace: {{ default $.root.Release.Namespace $item.namespace }}
  labels:
    {{- include "hby.labels" $ | nindent 4 }}
    {{- with $item.labels }}{{ toYaml . | nindent 4 }}{{- end }}
  {{- with $item.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- toYaml $item.spec | nindent 2 }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "hby.gatewayRoutes" -}}
{{- if .hby.gatewayRoutes.enabled }}
{{- range $route := .hby.gatewayRoutes.items }}
---
apiVersion: {{ default "gateway.networking.k8s.io/v1" $route.apiVersion }}
kind: {{ required "hby.gatewayRoutes.items[].kind is required" $route.kind }}
metadata:
  name: {{ default (include "hby.fullname" $) $route.name }}
  namespace: {{ default $.root.Release.Namespace $route.namespace }}
  labels:
    {{- include "hby.labels" $ | nindent 4 }}
    {{- with $route.labels }}{{ toYaml . | nindent 4 }}{{- end }}
  {{- if or $route.externalDNS $route.annotations }}
  annotations:
    {{- if and $route.externalDNS $route.hostnames }}
    external-dns.alpha.kubernetes.io/hostname: {{ join "," $route.hostnames | quote }}
    {{- end }}
    {{- with $route.annotations }}{{ toYaml . | nindent 4 }}{{- end }}
  {{- end }}
spec:
  {{- if and (eq $route.kind "HTTPRoute") $route.hostnames }}
  hostnames:
    {{- toYaml $route.hostnames | nindent 4 }}
  {{- end }}
  parentRefs:
    {{- toYaml $route.parentRefs | nindent 4 }}
  rules:
    {{- if $route.rules }}
    {{- range $rule := $route.rules }}
    - {{- with $rule.matches }}
      matches:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $rule.filters }}
      filters:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      backendRefs:
        {{- if $rule.backendRefs }}{{ toYaml $rule.backendRefs | nindent 8 }}{{- else }}
        - name: {{ include "hby.fullname" $ }}
          port: {{ $route.port }}
        {{- end }}
    {{- end }}
    {{- else }}
    - backendRefs:
        - name: {{ include "hby.fullname" $ }}
          port: {{ $route.port }}
    {{- end }}
{{- end }}
{{- end }}
{{- end -}}
