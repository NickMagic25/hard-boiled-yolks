{{- define "minecraft.env" -}}
- name: STARTUP
  value: {{ .Values.minecraft.startup | quote }}
- name: JAVA_OPTS
  value: {{ .Values.minecraft.javaOpts | quote }}
- name: SERVER_JARFILE
  value: {{ .Values.minecraft.serverJarFile | quote }}
- name: MINECRAFT_ARGS
  value: {{ .Values.minecraft.args | quote }}
{{- end -}}

{{- define "minecraft.extraVolumes" -}}
- name: installer
  configMap:
    name: {{ include "hby.fullname" (dict "root" . "hby" .Values.hby) }}-installer
    defaultMode: 0555
{{- end -}}

{{- define "minecraft.installContainer" -}}
- name: install-modrinth-pack
  image: "{{ .Values.installerImage.repository }}:{{ .Values.installerImage.tag }}"
  imagePullPolicy: {{ .Values.installerImage.pullPolicy }}
  command: [/bin/sh, /installer/install-modrinth-pack.sh]
  env:
    - {name: SERVER_DIR, value: {{ .Values.hby.persistence.mountPath | quote }}}
    - {name: SERVER_JARFILE, value: {{ .Values.minecraft.serverJarFile | quote }}}
    - {name: MINECRAFT_EULA, value: {{ ternary "true" "false" .Values.minecraft.eula | quote }}}
    - {name: MINECRAFT_EXTRA_JARS_JSON, value: {{ .Values.minecraft.extraJars | toJson | quote }}}
    - {name: MODRINTH_AUTH_ENABLED, value: {{ ternary "true" "false" .Values.modrinth.auth.enabled | quote }}}
    - {name: MODRINTH_AUTH_SCHEME, value: {{ .Values.modrinth.auth.scheme | quote }}}
    - {name: MODRINTH_API_BASE, value: {{ .Values.modrinth.apiBase | quote }}}
    - {name: MODRINTH_PROJECT_ID, value: {{ .Values.modrinth.projectId | quote }}}
    - {name: MODRINTH_VERSION_ID, value: {{ .Values.modrinth.versionId | quote }}}
    - {name: MODRINTH_VERSION_NUMBER, value: {{ .Values.modrinth.versionNumber | quote }}}
    - {name: MODRINTH_USER_AGENT, value: {{ .Values.modrinth.userAgent | quote }}}
    - {name: MODRINTH_INSTALL_POLICY, value: {{ .Values.modrinth.installPolicy | quote }}}
    - {name: MODRINTH_PACK_URL, value: {{ .Values.modrinth.pack.url | quote }}}
    - {name: MODRINTH_PACK_FILENAME, value: {{ .Values.modrinth.pack.filename | quote }}}
    - {name: MODRINTH_PACK_SHA512, value: {{ .Values.modrinth.pack.sha512 | quote }}}
    - {name: FABRIC_INSTALLER_VERSION, value: {{ .Values.modrinth.fabricInstallerVersion | quote }}}
    {{- if .Values.modrinth.auth.enabled }}
    - name: MODRINTH_TOKEN
      valueFrom:
        secretKeyRef:
          name: {{ required "modrinth.auth.existingSecret is required when auth is enabled" .Values.modrinth.auth.existingSecret }}
          key: {{ .Values.modrinth.auth.secretKey }}
    {{- end }}
  securityContext:
    {{- toYaml .Values.hby.securityContext | nindent 4 }}
  volumeMounts:
    - {name: data, mountPath: {{ .Values.hby.persistence.mountPath | quote }}}
    - {name: installer, mountPath: /installer, readOnly: true}
{{- end -}}
