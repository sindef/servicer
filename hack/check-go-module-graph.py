#!/usr/bin/env python3
"""Validate the resolved Go module graph against go.mod and go.sum.

Branch protection requires the `update-go_modules_graph` status check, but
GitHub's automatic dependency submission only reports that check for
default-branch pushes, so pull requests never carried it. This gate re-derives
the module graph the submission describes and fails when the committed go.sum
does not describe it. That is the drift a dependency bump leaves behind when
only the version in go.mod changes: the selected module then has no `h1:`
checksum, so the submitted graph and the build disagree.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

GO_SUM = Path("go.sum")


def fail(message: str) -> None:
    print(f"error: {message}", file=sys.stderr)
    raise SystemExit(1)


def run_go(*args: str) -> str:
    try:
        proc = subprocess.run(["go", *args], capture_output=True, text=True)
    except FileNotFoundError:
        fail("go toolchain not found on PATH")
    if proc.returncode != 0:
        detail = (proc.stderr or proc.stdout).strip()
        fail(f"go {' '.join(args)} failed: {detail}")
    return proc.stdout


def decode_stream(document: str, command: str) -> list[dict]:
    """Decode the concatenated JSON objects produced by `go ... -json`."""
    decoder = json.JSONDecoder()
    items: list[dict] = []
    index = 0
    while True:
        while index < len(document) and document[index].isspace():
            index += 1
        if index >= len(document):
            return items
        try:
            item, index = decoder.raw_decode(document, index)
        except json.JSONDecodeError as exc:
            fail(f"cannot parse go {command} output: {exc}")
        items.append(item)


def load_go_sum() -> dict[str, dict[str, set[tuple[str, str]]]]:
    if not GO_SUM.is_file():
        fail("go.sum is missing")
    sums: dict[str, dict[str, set[tuple[str, str]]]] = {}
    for lineno, line in enumerate(GO_SUM.read_text(encoding="utf-8").splitlines(), 1):
        if not line.strip():
            continue
        fields = line.split()
        if len(fields) != 3:
            fail(f"go.sum:{lineno}: expected three fields, got {len(fields)}")
        module, version, digest = fields
        kind = "go.mod" if version.endswith("/go.mod") else "module"
        version = version.removesuffix("/go.mod")
        entry = sums.setdefault(module, {"module": set(), "go.mod": set()})
        entry[kind].add((version, digest))
    return sums


def main() -> int:
    modules = decode_stream(run_go("list", "-m", "-json", "all"), "list -m")
    if not modules:
        fail("go list -m all resolved no modules")

    sums = load_go_sum()
    resolved: dict[str, str] = {}
    direct = indirect = 0
    missing: list[str] = []

    for module in modules:
        path = module.get("Path", "")
        version = module.get("Version", "")
        if not path:
            fail("go list -m returned a module without a path")
        if module.get("Main"):
            continue
        if not version:
            fail(f"module {path} resolved without a version")

        checksum_module, checksum_version = path, version
        replacement = module.get("Replace")
        if replacement:
            checksum_module = replacement.get("Path") or path
            checksum_version = replacement.get("Version") or ""
            if not checksum_version:
                # Local directory replacement: no module checksum applies.
                checksum_module = ""

        if checksum_module:
            known = sums.get(checksum_module, {}).get("module", set())
            if not any(known_version == checksum_version for known_version, _ in known):
                missing.append(f"{checksum_module}@{checksum_version}")

        resolved[path] = version
        if module.get("Indirect"):
            indirect += 1
        else:
            direct += 1

    if missing:
        fail(
            "go.sum has no module checksum for "
            + ", ".join(sorted(missing))
            + "; run 'go mod tidy', commit go.sum and refresh the license bundle"
        )

    requires = decode_stream(run_go("mod", "edit", "-json"), "mod edit")[0].get("Require") or []
    absent = sorted(requirement["Path"] for requirement in requires if requirement["Path"] not in resolved)
    if absent:
        fail("go.mod requires modules missing from the resolved graph: " + ", ".join(absent))

    print(
        f"Go module graph consistent: {len(resolved)} modules "
        f"({direct} direct, {indirect} indirect) with matching go.sum checksums."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
