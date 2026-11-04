#!/usr/bin/env python3
"""Fail when a ServiceMonitor in the rendered manifests selects no Service.

`ServiceMonitor.spec.selector.matchLabels` selects Services by their
`metadata.labels`, not by pod labels. A Service that carries no labels (or a
label set that does not cover the selector) makes the monitor match nothing, so
Prometheus never scrapes the endpoint and the alerts for that component are
silently dead. `deploy/bff.yaml` shipped exactly that bug: the `bff` Service had
no `metadata.labels` while `config/observability/service-monitors.yaml` selected
`app.kubernetes.io/name: bff`.

The checker reads rendered manifests (YAML) and verifies, for every
ServiceMonitor:

  1. at least one Service in the selected namespaces carries labels that
     contain every `spec.selector.matchLabels` pair, and
  2. each endpoint port name resolves to a port name on one of those Services.

It uses a small dependency-free YAML subset parser because the Validate job runs
plain `python3` without PyYAML.

Usage: check-servicemonitor-targets.py <rendered.yaml> [more.yaml ...]
Exit code 0 means every monitor has a scrape target, 1 means at least one
monitor is orphaned or unparsable.
"""

from __future__ import annotations

import sys


class ParseError(RuntimeError):
    pass


KEY_RE = r"[A-Za-z0-9_][A-Za-z0-9_./-]*"
BLOCK_SCALAR_MARKERS = ("|", "|-", "|+", ">", ">-", ">+")


def _strip_quotes(value: str) -> str:
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    return value


def _parse_scalar(text: str):
    text = text.strip()
    if text.startswith("[") and text.endswith("]"):
        inner = text[1:-1].strip()
        if not inner:
            return []
        return [_parse_scalar(part) for part in inner.split(",")]
    if text.startswith("{") and text.endswith("}"):
        inner = text[1:-1].strip()
        if not inner:
            return {}
        result = {}
        for part in inner.split(","):
            key, sep, value = part.partition(":")
            if not sep:
                raise ParseError(f"invalid flow mapping entry: {part!r}")
            result[_strip_quotes(key.strip())] = _parse_scalar(value)
        return result
    return _strip_quotes(text)


def _is_mapping_entry(text: str) -> bool:
    """True when a list item such as `- name: bff` starts an inline mapping."""
    key, sep, rest = text.partition(":")
    if not sep:
        return False
    if not key or any(ch.isspace() for ch in key):
        return False
    if not all(ch.isalnum() or ch in "_.-/=+*" for ch in key):
        return False
    # A key must be followed by end of line or a value, never by a path tail:
    # `- --listen=:8090` and `- http://x` are scalars, not mappings.
    return rest == "" or rest[0].isspace()


def _tokenize(lines, origin):
    tokens = []
    for number, raw in enumerate(lines, start=1):
        stripped = raw.strip()
        if not stripped or stripped.startswith("#"):
            continue
        indent = len(raw) - len(raw.lstrip(" "))
        tokens.append((indent, stripped, number))
    return tokens


def _parse_block(tokens, index, indent):
    if tokens[index][1].startswith("-"):
        return _parse_list(tokens, index, indent)
    return _parse_map(tokens, index, indent)


def _parse_map(tokens, index, indent):
    result = {}
    while index < len(tokens):
        line_indent, text, number = tokens[index]
        if line_indent < indent:
            break
        if line_indent > indent:
            raise ParseError(f"line {number}: unexpected indentation: {text!r}")
        if text.startswith("-"):
            break
        key, sep, rest = text.partition(":")
        if not sep:
            raise ParseError(f"line {number}: expected 'key: value': {text!r}")
        key = _strip_quotes(key.strip())
        rest = rest.strip()
        index += 1
        if rest in BLOCK_SCALAR_MARKERS:
            block = []
            while index < len(tokens) and tokens[index][0] > line_indent:
                block.append(tokens[index][1])
                index += 1
            result[key] = "\n".join(block)
        elif rest:
            result[key] = _parse_scalar(rest)
        elif index < len(tokens) and tokens[index][0] > line_indent:
            value, index = _parse_block(tokens, index, tokens[index][0])
            result[key] = value
        else:
            result[key] = None
    return result, index


def _parse_list(tokens, index, indent):
    result = []
    while index < len(tokens):
        line_indent, text, number = tokens[index]
        if line_indent != indent or not text.startswith("-"):
            break
        rest = text[1:].strip()
        index += 1
        if not rest:
            if index < len(tokens) and tokens[index][0] > line_indent:
                value, index = _parse_block(tokens, index, tokens[index][0])
                result.append(value)
            else:
                result.append(None)
            continue
        if _is_mapping_entry(rest):
            item_indent = line_indent + 2
            item_tokens = [(item_indent, rest, number)]
            while index < len(tokens) and tokens[index][0] >= item_indent:
                item_tokens.append(tokens[index])
                index += 1
            value, consumed = _parse_map(item_tokens, 0, item_indent)
            if consumed != len(item_tokens):
                raise ParseError(
                    f"line {number}: trailing content in list item: {text!r}"
                )
            result.append(value)
        else:
            result.append(_parse_scalar(rest))
    return result, index


def parse_documents(text, origin="<input>"):
    documents = []
    chunk = []
    for raw in text.splitlines():
        if raw.strip() == "---":
            documents.append(chunk)
            chunk = []
        else:
            chunk.append(raw)
    documents.append(chunk)

    parsed = []
    for chunk in documents:
        tokens = _tokenize(chunk, origin)
        if not tokens:
            continue
        try:
            document, consumed = _parse_map(tokens, 0, tokens[0][0])
        except ParseError as exc:
            raise ParseError(f"{origin}: {exc}") from exc
        if consumed != len(tokens):
            leftover = tokens[consumed]
            raise ParseError(f"{origin}: line {leftover[2]}: unparsed content: {leftover[1]!r}")
        if document:
            parsed.append(document)
    return parsed


def _mapping(value):
    return value if isinstance(value, dict) else {}


def _sequence(value):
    return value if isinstance(value, list) else []


def _selects(selector_labels, service_labels, match_expressions):
    for key, expected in selector_labels.items():
        if str(service_labels.get(key)) != str(expected):
            return False
    for expression in match_expressions:
        key = expression.get("key")
        operator = expression.get("operator", "In")
        values = [str(item) for item in _sequence(expression.get("values"))]
        present = key in service_labels
        actual = str(service_labels.get(key)) if present else None
        if operator == "In" and (not present or actual not in values):
            return False
        if operator == "NotIn" and present and actual in values:
            return False
        if operator == "Exists" and not present:
            return False
        if operator == "DoesNotExist" and present:
            return False
    return True


def _port_matches(port, service):
    ports = [entry for entry in _sequence(service.get("ports")) if isinstance(entry, dict)]
    names = {str(entry.get("name")) for entry in ports if entry.get("name") is not None}
    numbers = {str(entry.get("port")) for entry in ports if entry.get("port") is not None}
    text = str(port)
    if text.isdigit():
        return text in numbers
    return text in names


def _describe(document):
    metadata = _mapping(document.get("metadata"))
    return f"{metadata.get('namespace', 'default')}/{metadata.get('name', '<unnamed>')}"


def check(documents):
    services = []
    monitors = []
    for document in documents:
        kind = document.get("kind")
        metadata = _mapping(document.get("metadata"))
        if kind == "Service":
            services.append(
                {
                    "namespace": str(metadata.get("namespace", "default")),
                    "name": str(metadata.get("name", "<unnamed>")),
                    "labels": {str(k): str(v) for k, v in _mapping(metadata.get("labels")).items()},
                    "ports": _sequence(_mapping(document.get("spec")).get("ports")),
                }
            )
        elif kind == "ServiceMonitor":
            monitors.append(document)

    errors = []
    for monitor in monitors:
        metadata = _mapping(monitor.get("metadata"))
        spec = _mapping(monitor.get("spec"))
        namespace = str(metadata.get("namespace", "default"))
        name = f"{namespace}/{metadata.get('name', '<unnamed>')}"

        selector = _mapping(spec.get("selector"))
        match_labels = {
            str(key): str(value) for key, value in _mapping(selector.get("matchLabels")).items()
        }
        match_expressions = [expr for expr in _sequence(selector.get("matchExpressions")) if isinstance(expr, dict)]

        namespace_selector = _mapping(spec.get("namespaceSelector"))
        if namespace_selector.get("any") is True:
            selected_namespaces = None
        else:
            names = [str(item) for item in _sequence(namespace_selector.get("matchNames"))]
            selected_namespaces = set(names) if names else {namespace}

        matched = []
        for service in services:
            if selected_namespaces is not None and service["namespace"] not in selected_namespaces:
                continue
            if _selects(match_labels, service["labels"], match_expressions):
                matched.append(service)

        if not matched:
            wanted = ", ".join(f"{key}={value}" for key, value in sorted(match_labels.items())) or "<empty selector>"
            scope = "all namespaces" if selected_namespaces is None else ", ".join(sorted(selected_namespaces))
            errors.append(
                f"ServiceMonitor {name} selects no Service: selector [{wanted}] matched no Service "
                f"metadata.labels in namespace(s) {scope}. Add the selector labels to the Service's "
                f"metadata.labels, not only to its pod template."
            )
            continue

        endpoints = [entry for entry in _sequence(spec.get("endpoints")) if isinstance(entry, dict)]
        for endpoint in endpoints:
            port = endpoint.get("port")
            if port is None or port == "":
                continue
            if not any(_port_matches(port, service) for service in matched):
                candidates = ", ".join(sorted(f"{svc['namespace']}/{svc['name']}" for svc in matched))
                errors.append(
                    f"ServiceMonitor {name} endpoint port {port!r} is not a port name on the selected "
                    f"Service(s) {candidates}."
                )
    return errors


def main(argv):
    if len(argv) < 2:
        print("usage: check-servicemonitor-targets.py <rendered.yaml> [...]", file=sys.stderr)
        return 2

    documents = []
    for path in argv[1:]:
        try:
            with open(path, encoding="utf-8") as handle:
                documents.extend(parse_documents(handle.read(), origin=path))
        except OSError as exc:
            print(f"error: cannot read {path}: {exc}", file=sys.stderr)
            return 2
        except ParseError as exc:
            print(f"error: {exc}", file=sys.stderr)
            return 1

    errors = check(documents)
    for message in errors:
        print(f"error: {message}", file=sys.stderr)
    if errors:
        return 1

    monitors = sum(1 for document in documents if document.get("kind") == "ServiceMonitor")
    services = sum(1 for document in documents if document.get("kind") == "Service")
    print(f"service monitor targets ok: {monitors} ServiceMonitor(s) resolve against {services} Service(s)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
