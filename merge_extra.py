import json

monitoring = open('deploy/monitoring-rules.yaml').read()
rules = open('config/observability/prometheus-rules.yaml').read()

lines = monitoring.splitlines()
blocks = []
current = None
for line in lines:
    if line.startswith('        - alert:'):
        current = [line]
        blocks.append(current)
    elif line.startswith('          ') and current is not None:
        current.append(line)
    else:
        current = None
names = [b[0].split(': ',1)[1].strip() for b in blocks if len(b)>1]
print("deploy alerts:", names)

import re
names_slos = re.findall(r'- alert:\s*(\S+)', rules)
print("slos alerts:", names_slos)

extra = [n for n in names if not any(s.startswith(n.rsplit('High')[0]) for s in names_slos)]
print("names not in slos:", extra)
