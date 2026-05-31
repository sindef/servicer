#!/usr/bin/env bash
set -euo pipefail

mode="${1:-manifest}"
smoke_manifest=""
had_servicer_system=0
had_monitoring=0
had_ingress_nginx=0
had_np_smoke_client=0

manifest_smoke() {
  local rendered
  rendered="$(mktemp)"

  kubectl kustomize deploy > "${rendered}"

  count="$(grep -c '^kind: NetworkPolicy$' "${rendered}" || true)"
  if [[ "${count}" -lt 4 ]]; then
    echo "expected at least 4 NetworkPolicy resources in deploy manifests, found ${count}" >&2
    exit 1
  fi

  grep -q 'name: default-deny' "${rendered}" || {
    echo "missing default-deny NetworkPolicy in deploy manifests" >&2
    exit 1
  }

  grep -q 'namespace: servicer-system' "${rendered}" || {
    echo "expected servicer-system namespace in NetworkPolicy manifests" >&2
    exit 1
  }

  echo "NetworkPolicy manifest smoke passed (${count} policies rendered)."
  rm -f "${rendered}"
}

wait_for_pod_ready() {
  local namespace="$1"
  local pod="$2"
  kubectl wait -n "${namespace}" --for=condition=Ready "pod/${pod}" --timeout=120s >/dev/null
}

expect_success() {
  local message="$1"
  shift
  if ! "$@"; then
    echo "failed: ${message}" >&2
    exit 1
  fi
}

expect_failure() {
  local message="$1"
  shift
  if "$@"; then
    echo "expected denial but call succeeded: ${message}" >&2
    exit 1
  fi
}

wait_for_namespace_stable() {
  local namespace="$1"
  while kubectl get ns "${namespace}" >/dev/null 2>&1; do
    phase="$(kubectl get ns "${namespace}" -o jsonpath='{.status.phase}')"
    if [[ "${phase}" != "Terminating" ]]; then
      return 0
    fi
    sleep 1
  done
}

runtime_smoke() {
  smoke_manifest="$(mktemp)"

  if kubectl get ns servicer-system >/dev/null 2>&1; then
    had_servicer_system=1
    existing_pods="$(kubectl get pods -n servicer-system --no-headers 2>/dev/null | wc -l | tr -d " ")"
    if [[ "${existing_pods}" != "0" && "${SERVICER_NETWORKPOLICY_SMOKE_ALLOW_NONEMPTY:-false}" != "true" ]]; then
      echo "runtime smoke requires an empty servicer-system namespace; set SERVICER_NETWORKPOLICY_SMOKE_ALLOW_NONEMPTY=true to override" >&2
      exit 1
    fi
  fi
  wait_for_namespace_stable servicer-system
  kubectl create namespace servicer-system --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  if kubectl get ns monitoring >/dev/null 2>&1; then
    had_monitoring=1
  fi
  wait_for_namespace_stable monitoring
  kubectl create namespace monitoring --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  if kubectl get ns ingress-nginx >/dev/null 2>&1; then
    had_ingress_nginx=1
  fi
  wait_for_namespace_stable ingress-nginx
  kubectl create namespace ingress-nginx --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  if kubectl get ns np-smoke-client >/dev/null 2>&1; then
    had_np_smoke_client=1
  fi
  wait_for_namespace_stable np-smoke-client
  kubectl create namespace np-smoke-client --dry-run=client -o yaml | kubectl apply -f - >/dev/null

  cat > "${smoke_manifest}" <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: web
  namespace: servicer-system
  labels:
    app.kubernetes.io/name: web
spec:
  containers:
    - name: web
      image: busybox:1.36
      command: ["/bin/sh", "-ec"]
      args:
        - |
          mkdir -p /srv/web
          echo ok > /srv/web/index.html
          httpd -f -p 8080 -h /srv/web &
          sleep 36000
---
apiVersion: v1
kind: Service
metadata:
  name: web
  namespace: servicer-system
spec:
  selector:
    app.kubernetes.io/name: web
  ports:
    - name: http
      port: 80
      targetPort: 8080
---
apiVersion: v1
kind: Pod
metadata:
  name: bff
  namespace: servicer-system
  labels:
    app.kubernetes.io/name: bff
spec:
  containers:
    - name: bff
      image: busybox:1.36
      command: ["/bin/sh", "-ec"]
      args:
        - |
          mkdir -p /srv/api/api /srv/metrics
          echo ok > /srv/api/api/healthz
          printf "# HELP smoke_metric smoke\n# TYPE smoke_metric counter\nsmoke_metric 1\n" > /srv/metrics/metrics
          httpd -f -p 8090 -h /srv/api &
          httpd -f -p 9090 -h /srv/metrics &
          sleep 36000
---
apiVersion: v1
kind: Service
metadata:
  name: bff
  namespace: servicer-system
spec:
  selector:
    app.kubernetes.io/name: bff
  ports:
    - name: http
      port: 8090
      targetPort: 8090
    - name: metrics
      port: 9090
      targetPort: 9090
---
apiVersion: v1
kind: Pod
metadata:
  name: manager
  namespace: servicer-system
  labels:
    app.kubernetes.io/name: manager
spec:
  containers:
    - name: manager
      image: busybox:1.36
      command: ["/bin/sh", "-ec"]
      args:
        - |
          mkdir -p /srv/metrics /srv/webhook
          printf "# HELP manager_smoke_metric smoke\n# TYPE manager_smoke_metric counter\nmanager_smoke_metric 1\n" > /srv/metrics/metrics
          echo ok > /srv/webhook/index.html
          httpd -f -p 8080 -h /srv/metrics &
          httpd -f -p 9443 -h /srv/webhook &
          sleep 36000
---
apiVersion: v1
kind: Service
metadata:
  name: manager-metrics
  namespace: servicer-system
spec:
  selector:
    app.kubernetes.io/name: manager
  ports:
    - name: metrics
      port: 8080
      targetPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: servicer-webhook-service
  namespace: servicer-system
spec:
  selector:
    app.kubernetes.io/name: manager
  ports:
    - name: https
      port: 443
      targetPort: 9443
---
apiVersion: v1
kind: Pod
metadata:
  name: ingress-probe
  namespace: ingress-nginx
spec:
  containers:
    - name: ingress-probe
      image: busybox:1.36
      command: ["/bin/sh", "-ec"]
      args: ["sleep 36000"]
---
apiVersion: v1
kind: Pod
metadata:
  name: monitoring-probe
  namespace: monitoring
spec:
  containers:
    - name: monitoring-probe
      image: busybox:1.36
      command: ["/bin/sh", "-ec"]
      args: ["sleep 36000"]
---
apiVersion: v1
kind: Pod
metadata:
  name: webhook-probe
  namespace: np-smoke-client
spec:
  containers:
    - name: webhook-probe
      image: busybox:1.36
      command: ["/bin/sh", "-ec"]
      args: ["sleep 36000"]
EOF

cleanup() {
    if [[ -n "${smoke_manifest}" ]]; then
      kubectl delete -f "${smoke_manifest}" --ignore-not-found >/dev/null 2>&1 || true
      rm -f "${smoke_manifest}"
      smoke_manifest=""
    fi
    if [[ "${had_np_smoke_client}" == "0" ]]; then
      kubectl delete ns np-smoke-client --ignore-not-found >/dev/null 2>&1 || true
    fi
    if [[ "${had_ingress_nginx}" == "0" ]]; then
      kubectl delete ns ingress-nginx --ignore-not-found >/dev/null 2>&1 || true
    fi
    if [[ "${had_monitoring}" == "0" ]]; then
      kubectl delete ns monitoring --ignore-not-found >/dev/null 2>&1 || true
    fi
    if [[ "${had_servicer_system}" == "0" ]]; then
      kubectl delete ns servicer-system --ignore-not-found >/dev/null 2>&1 || true
    fi
  }
  trap cleanup EXIT

  kubectl apply -f "${smoke_manifest}" >/dev/null
  kubectl apply -f deploy/network-policies.yaml >/dev/null

  if ! timeout 150s kubectl wait -n kube-system --for=condition=Ready pod -l k8s-app=kube-dns --timeout=120s >/dev/null; then
    echo "kube-dns was not ready; runtime smoke requires a healthy cluster DNS/control plane" >&2
    exit 1
  fi
  wait_for_pod_ready servicer-system web
  wait_for_pod_ready servicer-system bff
  wait_for_pod_ready servicer-system manager
  wait_for_pod_ready ingress-nginx ingress-probe
  wait_for_pod_ready monitoring monitoring-probe
  wait_for_pod_ready np-smoke-client webhook-probe

  web_svc_ip="$(kubectl get svc -n servicer-system web -o jsonpath='{.spec.clusterIP}')"
  bff_svc_ip="$(kubectl get svc -n servicer-system bff -o jsonpath='{.spec.clusterIP}')"
  manager_metrics_svc_ip="$(kubectl get svc -n servicer-system manager-metrics -o jsonpath='{.spec.clusterIP}')"
  webhook_svc_ip="$(kubectl get svc -n servicer-system servicer-webhook-service -o jsonpath='{.spec.clusterIP}')"

  expect_success "ingress can reach web service" \
    kubectl exec -n ingress-nginx ingress-probe -- wget -qO- "http://${web_svc_ip}" >/dev/null
  expect_success "web can reach bff API service on 8090" \
    kubectl exec -n servicer-system web -- wget -qO- "http://${bff_svc_ip}:8090/api/healthz" >/dev/null
  expect_success "monitoring can scrape bff metrics on 9090" \
    kubectl exec -n monitoring monitoring-probe -- wget -qO- "http://${bff_svc_ip}:9090/metrics" >/dev/null
  expect_success "monitoring can scrape manager metrics on 8080" \
    kubectl exec -n monitoring monitoring-probe -- wget -qO- "http://${manager_metrics_svc_ip}:8080/metrics" >/dev/null
  expect_success "webhook service is reachable from arbitrary namespace" \
    kubectl exec -n np-smoke-client webhook-probe -- nc -z -w 3 "${webhook_svc_ip}" 443
  expect_success "web pod can resolve DNS through kube-system DNS" \
    kubectl exec -n servicer-system web -- nslookup kubernetes.default.svc.cluster.local >/dev/null

  expect_failure "monitoring must not reach bff API port 8090" \
    kubectl exec -n monitoring monitoring-probe -- wget -qO- "http://${bff_svc_ip}:8090/api/healthz" >/dev/null
  expect_failure "web egress must not reach Kubernetes API on 443" \
    kubectl exec -n servicer-system web -- nc -z -w 3 kubernetes.default.svc.cluster.local 443

  echo "NetworkPolicy runtime smoke passed (allow and deny paths verified)."
}

case "${mode}" in
  manifest|--manifest)
    manifest_smoke
    ;;
  runtime|--runtime)
    manifest_smoke
    runtime_smoke
    ;;
  *)
    echo "usage: $0 [manifest|runtime]" >&2
    exit 1
    ;;
esac
