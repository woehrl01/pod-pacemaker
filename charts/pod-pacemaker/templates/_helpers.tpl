{{/* arguments of cni-init, shared by the init-cni init container and the cni-config-sync container */}}
{{- define "pod-pacemaker.cniInitArgs" -}}
- "--cni-bin-dir=/opt/cni/bin"
- "--cni-config-dir=/etc/cni/net.d"
- "--max-wait-time-in-seconds={{ .Values.cni.maxWaitTimeInSeconds }}"
- "--primary-config-name={{ .Values.cni.primaryName }}"
- "--merged-config-name={{ .Values.cni.mergedName }}"
- "--namespace-exclusions={{ join "," .Values.namespaceExclusions }}"
- "--success-on-connection-timeout={{ .Values.cni.successOnConnectionTimeout }}"
- "--daemon-socket={{ .Values.daemon.socketFile }}"
- "--disable-throttle={{ .Values.cni.disableThrottle }}"
{{- end }}
