{{- define "steamcmd.env" -}}
- name: HBY_STEAMCMD_MODE
  value: run
- name: SRCDS_APPID
  value: {{ required "steamcmd.appId is required" .Values.steamcmd.appId | quote }}
- name: STARTUP
  value: {{ required "steamcmd.startup is required" .Values.steamcmd.startup | quote }}
{{- end -}}

{{- define "steamcmd.bootstrapVolume" -}}
- name: bootstrap
  configMap:
    name: {{ include "hby.fullname" (dict "root" . "hby" .Values.hby) }}-bootstrap
    defaultMode: 0555
{{- end -}}

{{- define "steamcmd.installContainers" -}}
- name: steam-install
  image: "{{ .Values.hby.image.repository }}:{{ .Values.hby.image.tag }}"
  imagePullPolicy: {{ .Values.hby.image.pullPolicy }}
  command: [/usr/bin/entrypoint.sh, install]
  env:
    - name: SRCDS_APPID
      value: {{ .Values.steamcmd.appId | quote }}
    - name: AUTO_UPDATE
      value: {{ ternary "1" "0" .Values.steamcmd.autoUpdate | quote }}
    - name: VALIDATE
      value: {{ ternary "1" "0" .Values.steamcmd.validate | quote }}
    - name: UPDATE_STEAMWORKS
      value: {{ ternary "1" "0" .Values.steamcmd.updateSteamworks | quote }}
    - name: WINDOWS_INSTALL
      value: "0"
    - name: STEAM_USER
      value: {{ .Values.steamcmd.credentials.user | quote }}
    - name: SRCDS_BETAID
      value: {{ .Values.steamcmd.betaId | quote }}
    {{- with .Values.steamcmd.betaPassword }}
    - name: SRCDS_BETAPASS
      value: {{ . | quote }}
    {{- end }}
    - name: INSTALL_FLAGS
      value: {{ .Values.steamcmd.installFlags | quote }}
    {{- with .Values.steamcmd.installEnv }}{{ toYaml . | nindent 4 }}{{- end }}
  {{- with .Values.steamcmd.credentials.envFrom }}
  envFrom:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  securityContext:
    {{- toYaml .Values.hby.securityContext | nindent 4 }}
  volumeMounts:
    - name: data
      mountPath: /home/container
{{- if .Values.steamcmd.bootstrap.enabled }}
- name: bootstrap
  image: {{ default (printf "%s:%s" .Values.hby.image.repository .Values.hby.image.tag) .Values.steamcmd.bootstrap.image | quote }}
  imagePullPolicy: {{ .Values.hby.image.pullPolicy }}
  command: [/bin/bash, /bootstrap/bootstrap.sh]
  env:
    {{- with .Values.steamcmd.bootstrap.env }}{{ toYaml . | nindent 4 }}{{- end }}
  {{- with .Values.steamcmd.bootstrap.envFrom }}
  envFrom:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  securityContext:
    {{- toYaml .Values.hby.securityContext | nindent 4 }}
  volumeMounts:
    - name: data
      mountPath: /home/container
    - name: bootstrap
      mountPath: /bootstrap
      readOnly: true
{{- end }}
{{- with .Values.hby.workload.extraInitContainers }}{{ toYaml . }}{{- end }}
{{- end -}}
