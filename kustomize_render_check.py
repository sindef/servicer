#!/usr/bin/env python3
"""Render every kustomization with a pin-variant kustomize build via go run."""
import subprocess
import sys

targets = ["deploy", "config/deploy", "config/observability", "config/samples", "config/backup"]
failed = False
for t in targets:
    p = None
    cmd = ["go", "run", "sigs.k8s.io/kustomize/kustomize/v5@v5.4.3", "build", t, "--load-restrictor", "LoadRestrictionsNone"]
    p = subprocess.run(cmd, capture_output=True, text=True)
    ok = p.returncode == 0
    out = p.stdout
    prom = out.count("kind: PrometheusRule")
    sm = out.count("kind: ServiceMonitor")
    print(f"{t}: exit={p.returncode} PrometheusRule={prom} ServiceMonitor={sm}")
    if not ok:
        failed = True
        print(p.stderr[-1500:])
sys.exit(1 if failed else 0)
