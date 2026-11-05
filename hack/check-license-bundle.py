#!/usr/bin/env python3
"""Offline self-consistency gate for the committed third-party license bundle.

`hack/generate-third-party-licenses.sh` writes `dist/THIRD_PARTY_LICENSES/` from
the resolved Go module graph and the installed npm package tree. Regeneration
needs access to the Go module proxy and the npm registry, so the `Release
hygiene` workflow owns the registry-backed drift check: it regenerates the
bundle and fails when the committed copy would change.

This script is the registry-free companion gate that runs on every pull request
(`Build` -> `Validate`). It verifies that the committed bundle is internally
consistent, so a hand-edited or half-committed refresh is caught immediately
instead of waiting for the weekly regeneration job:

  * the bundle mirrors `LICENSE` (as `SERVICER-LICENSE`) and
    `THIRD_PARTY_NOTICES.md` byte for byte
  * every `web/PACKAGE_LICENSES.tsv` row resolves to a license directory, or is
    recorded as an approved exception, and every license directory has a row
  * the committed approved exceptions match the reviewed set in
    `hack/generate-third-party-licenses.sh`
  * approved exceptions no longer ship a license file upstream
  * every license directory holds at least one upstream license/notice file
  * manifests are sorted, unique, and free of malformed rows
  * no `web/MISSING_LICENSE_FILES.tsv`, which would mean the bundle was
    committed with unapproved missing license files
  * the bundle records the versions `go.mod` and `web/package-lock.json`
    currently resolve: every direct Go requirement has a license directory for
    the required version (or a recorded no-license-file entry), and every
    `web/PACKAGE_LICENSES.tsv` row names a package with a version the lockfile
    still resolves. This needs no registry access, so a dependency bump that
    forgets to refresh the bundle fails on the pull request that introduces it
    instead of waiting for the weekly `Release hygiene` regeneration.

What it does not do: prove the bundle is exactly what the generator would
write. Adding or removing a package without a version change, and any drift
inside a license file, is still only caught by the generator output comparison
in the `Release hygiene` workflow, which needs registry access.

Usage:
    python3 hack/check-license-bundle.py [bundle-directory]
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_BUNDLE = ROOT / "dist" / "THIRD_PARTY_LICENSES"
GENERATOR = ROOT / "hack" / "generate-third-party-licenses.sh"
GO_MOD = ROOT / "go.mod"
WEB_LOCK = ROOT / "web" / "package-lock.json"

LICENSE_FILE_PATTERN = re.compile(r"^(licen[sc]e|copying|notice)", re.IGNORECASE)
SANITIZE_PATTERN = re.compile(r"[/:@+]")
GO_ENTRY_PATTERN = re.compile(r"^.+_v[0-9]")
APPROVED_BLOCK_PATTERN = re.compile(r"const approved = new Set\(\[(.*?)\]\)", re.DOTALL)
APPROVED_ENTRY_PATTERN = re.compile(r"['\"]([^'\"]+)['\"]")
GO_REQUIRE_LINE_PATTERN = re.compile(r"^([^\s()]+)\s+(v[^\s]+)$")

WEB_MANIFEST = "web/PACKAGE_LICENSES.tsv"
WEB_APPROVED = "web/APPROVED_LICENSE_EXCEPTIONS.tsv"
WEB_MISSING = "web/MISSING_LICENSE_FILES.tsv"
GO_MISSING = "go/MISSING_LICENSE_FILES.tsv"
ALLOWED_ROOT_ENTRIES = {"SERVICER-LICENSE", "THIRD_PARTY_NOTICES.md", "go", "web", "operators"}
MIRRORED_FILES = (
    ("LICENSE", "SERVICER-LICENSE"),
    ("THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.md"),
)

problems: list[str] = []


def fail(message: str) -> None:
    problems.append(message)


def relative(path: Path) -> str:
    try:
        return str(path.relative_to(ROOT))
    except ValueError:
        return str(path)


def sanitize(value: str) -> str:
    return SANITIZE_PATTERN.sub("_", value)


def read_rows(path: Path, columns: int) -> list[list[str]]:
    rows: list[list[str]] = []
    lines = path.read_text(encoding="utf-8").splitlines()
    for number, line in enumerate(lines, start=1):
        if line.strip() == "":
            fail(f"{relative(path)}:{number}: blank line is not allowed")
            continue
        fields = line.split("\t")
        if len(fields) != columns or any(field == "" for field in fields):
            fail(f"{relative(path)}:{number}: expected {columns} non-empty tab-separated fields, got {line!r}")
            continue
        rows.append(fields)
    if lines != sorted(lines):
        fail(f"{relative(path)}: entries are not sorted; regenerate the bundle instead of editing it")
    return rows


def license_files(directory: Path) -> list[Path]:
    return sorted(path for path in directory.rglob("*") if path.is_file())


def check_license_directory(directory: Path) -> int:
    files = license_files(directory)
    if not files:
        fail(f"{relative(directory)}: no license files; regenerate the bundle instead of committing an empty directory")
        return 0
    for path in files:
        if not LICENSE_FILE_PATTERN.match(path.name):
            fail(f"{relative(path)}: unexpected file in a license directory; the generator only copies license, copying and notice files")
    return len(files)


def check_mirrored_file(source_name: str, bundle_name: str) -> None:
    source = ROOT / source_name
    mirrored = BUNDLE / bundle_name
    if not source.is_file():
        fail(f"{source_name}: missing from the repository")
        return
    if not mirrored.is_file():
        fail(f"dist/THIRD_PARTY_LICENSES/{bundle_name}: missing; the generator copies it from {source_name}")
        return
    if source.read_bytes() != mirrored.read_bytes():
        fail(f"dist/THIRD_PARTY_LICENSES/{bundle_name}: does not match {source_name}; refresh the bundle")


def generator_approved_exceptions() -> set[str]:
    """Reviewed npm exception set hard-coded in the generator script."""
    if not GENERATOR.is_file():
        fail(f"{relative(GENERATOR)}: missing; cannot verify the approved exception set")
        return set()
    match = APPROVED_BLOCK_PATTERN.search(GENERATOR.read_text(encoding="utf-8"))
    if match is None:
        fail(
            f"{relative(GENERATOR)}: cannot locate the reviewed npm approved exception set "
            "(expected `const approved = new Set([...])`)"
        )
        return set()
    return set(APPROVED_ENTRY_PATTERN.findall(match.group(1)))


def go_direct_requirements() -> dict[str, str]:
    """Direct (non-indirect) `go.mod` requirements, excluding replaced modules."""
    if not GO_MOD.is_file():
        fail("go.mod: missing; cannot verify the bundle against the resolved dependency graph")
        return {}
    requirements: dict[str, str] = {}
    replaced: set[str] = set()
    block: str | None = None
    for number, raw in enumerate(GO_MOD.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw.strip()
        if line.startswith("//"):
            continue
        indirect = re.search(r"//\s*indirect\b", line) is not None
        code = line.split("//", 1)[0].strip()
        if code == "":
            continue
        if block is None:
            if code in ("require (", "replace (", "exclude (", "retract ("):
                block = code.split()[0]
                continue
            if code.startswith("replace "):
                replaced.add(code.split()[1])
                continue
            if not code.startswith("require "):
                continue
            code = code[len("require "):].strip()
        else:
            if code == ")":
                block = None
                continue
            if block == "replace":
                replaced.add(code.split()[0])
                continue
            if block != "require":
                continue
        if indirect:
            continue
        match = GO_REQUIRE_LINE_PATTERN.match(code)
        if match is None:
            fail(f"go.mod:{number}: cannot parse requirement {line!r}")
            continue
        requirements[match.group(1)] = match.group(2)
    return {module: version for module, version in requirements.items() if module not in replaced}


def go_bundle_versions() -> tuple[set[str], set[str]]:
    """License directory names and recorded no-license-file modules in the Go bundle."""
    go_dir = BUNDLE / "go"
    directories = {entry.name for entry in go_dir.iterdir() if entry.is_dir()}
    no_license_file: set[str] = set()
    missing = go_dir / "MISSING_LICENSE_FILES.tsv"
    if missing.is_file():
        for module, version, note in read_rows(missing, 3):
            if note == "NO_LICENSE_FILE_FOUND":
                no_license_file.add(f"{module}@{version}")
    return directories, no_license_file


def check_go_bundle_versions(directories: set[str], no_license_file: set[str]) -> None:
    for module, version in sorted(go_direct_requirements().items()):
        key = f"{module}@{version}"
        if sanitize(key) in directories or key in no_license_file:
            continue
        fail(
            f"dist/THIRD_PARTY_LICENSES/go/{sanitize(key)}: missing license directory for the "
            f"go.mod requirement {module} {version}; the committed bundle records an older "
            "dependency graph. Regenerate with ./hack/generate-third-party-licenses.sh"
        )


def web_lock_versions() -> dict[str, set[str]] | None:
    """Package name to resolved version set from `web/package-lock.json`."""
    if not WEB_LOCK.is_file():
        fail("web/package-lock.json: missing; cannot verify the bundle against the resolved npm graph")
        return None
    try:
        lock = json.loads(WEB_LOCK.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        fail(f"web/package-lock.json: cannot parse: {error}")
        return None
    resolved: dict[str, set[str]] = {}
    for key, entry in (lock.get("packages") or {}).items():
        if not isinstance(entry, dict) or entry.get("link") is True:
            continue
        version = entry.get("version")
        if not isinstance(version, str):
            continue
        name = entry.get("name")
        if not isinstance(name, str):
            marker = key.rfind("node_modules/")
            if marker < 0:
                continue
            name = key[marker + len("node_modules/"):]
        resolved.setdefault(name, set()).add(version)
    return resolved


def check_web_bundle_versions(rows: list[list[str]]) -> None:
    resolved = web_lock_versions()
    if resolved is None:
        return
    for name, version, _license in rows:
        versions = resolved.get(name)
        if versions is None:
            fail(
                f"{WEB_MANIFEST}: {name}@{version} is no longer resolved by web/package-lock.json; "
                "the committed bundle is stale. Regenerate with ./hack/generate-third-party-licenses.sh"
            )
            continue
        if version not in versions:
            fail(
                f"{WEB_MANIFEST}: records {name}@{version} but web/package-lock.json resolves "
                f"{name}@{', '.join(sorted(versions))}; the committed bundle is stale. "
                "Regenerate with ./hack/generate-third-party-licenses.sh"
            )


def check_web_bundle() -> tuple[int, int]:
    manifest_path = BUNDLE / WEB_MANIFEST
    approved_path = BUNDLE / WEB_APPROVED
    if not manifest_path.is_file():
        fail(f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: missing")
    if not approved_path.is_file():
        fail(f"dist/THIRD_PARTY_LICENSES/{WEB_APPROVED}: missing")
    if (BUNDLE / WEB_MISSING).is_file():
        fail(
            f"dist/THIRD_PARTY_LICENSES/{WEB_MISSING}: present; the bundle was committed with "
            "unapproved missing license files. Review them and extend the approved exception set "
            "in hack/generate-third-party-licenses.sh before refreshing the bundle"
        )
    if not manifest_path.is_file() or not approved_path.is_file():
        return (0, 0)

    rows = read_rows(manifest_path, 3)
    approved_rows = read_rows(approved_path, 3)
    check_web_bundle_versions(rows)

    row_keys: list[str] = []
    expected_dirs: dict[str, str] = {}
    for name, version, _license in rows:
        key = f"{name}@{version}"
        if key in row_keys:
            fail(f"{WEB_MANIFEST}: duplicate entry for {key}")
        row_keys.append(key)
        expected_dirs[sanitize(key)] = key

    actual_dirs = sorted(path for path in (BUNDLE / "web").iterdir() if path.is_dir())
    actual_dir_names = {path.name for path in actual_dirs}

    for directory in actual_dirs:
        if directory.name not in expected_dirs:
            fail(f"{relative(directory)}: no {WEB_MANIFEST} row; regenerate the bundle instead of adding directories")
        else:
            check_license_directory(directory)

    approved_keys: list[str] = []
    for name, version, _license in approved_rows:
        key = f"{name}@{version}"
        if key in approved_keys:
            fail(f"{WEB_APPROVED}: duplicate entry for {key}")
        approved_keys.append(key)
        if key not in row_keys:
            fail(f"{WEB_APPROVED}: {key} has no {WEB_MANIFEST} row; the generated exception set is derived from the manifest")
        if sanitize(key) in actual_dir_names:
            fail(f"{WEB_APPROVED}: {key} now ships a license file, so it must be dropped from the approved exceptions")

    reviewed = generator_approved_exceptions()
    for key in sorted(reviewed - set(approved_keys)):
        fail(
            f"{WEB_APPROVED}: missing approved exception {key}; the committed bundle does not match the reviewed "
            "set in hack/generate-third-party-licenses.sh"
        )
    for key in sorted(set(approved_keys) - reviewed):
        fail(
            f"{WEB_APPROVED}: unexpected approved exception {key}; add it to the reviewed set in "
            "hack/generate-third-party-licenses.sh or refresh the bundle"
        )

    for key in row_keys:
        if sanitize(key) not in actual_dir_names and key not in approved_keys:
            fail(
                f"{relative(BUNDLE / 'web' / sanitize(key))}: missing license directory for {key} and "
                f"no entry in {WEB_APPROVED}"
            )

    return (len(row_keys), len(actual_dirs))


def check_go_bundle() -> int:
    go_dir = BUNDLE / "go"
    entries = sorted(go_dir.iterdir())
    checked = 0
    for entry in entries:
        if entry.is_dir():
            if not GO_ENTRY_PATTERN.match(entry.name):
                fail(f"{relative(entry)}: directory name does not look like a sanitized module@version entry")
            check_license_directory(entry)
            checked += 1
        elif entry.name != "MISSING_LICENSE_FILES.tsv":
            fail(f"{relative(entry)}: unexpected file in the Go bundle; the generator only writes per-module license directories")
    missing = go_dir / "MISSING_LICENSE_FILES.tsv"
    if missing.is_file():
        for module, version, note in read_rows(missing, 3):
            if note != "NO_LICENSE_FILE_FOUND":
                fail(f"{relative(missing)}: unexpected record {module}@{version} {note!r}")
            if sanitize(f"{module}@{version}") in {entry.name for entry in entries}:
                fail(f"{relative(go_dir / sanitize(module + '@' + version))}: has a license file but is listed in {missing.name}")
    check_go_bundle_versions(*go_bundle_versions())
    return checked


def main(argv: list[str]) -> int:
    global BUNDLE
    if len(argv) > 2:
        print("usage: check-license-bundle.py [bundle-directory]", file=sys.stderr)
        return 2
    BUNDLE = Path(argv[1]).resolve() if len(argv) == 2 else DEFAULT_BUNDLE

    if not BUNDLE.is_dir():
        print(f"error: missing bundle directory: {relative(BUNDLE)}", file=sys.stderr)
        return 1

    for entry in sorted(BUNDLE.iterdir()):
        if entry.name not in ALLOWED_ROOT_ENTRIES:
            fail(f"{relative(entry)}: unexpected bundle entry; regenerate the bundle instead of adding files")
    for required in ("SERVICER-LICENSE", "THIRD_PARTY_NOTICES.md", "operators/README.md"):
        path = BUNDLE / required
        if not path.is_file() or path.stat().st_size == 0:
            fail(f"{relative(path)}: missing or empty")
    if not (BUNDLE / "go").is_dir():
        fail(f"{relative(BUNDLE / 'go')}: missing")
    if not (BUNDLE / "web").is_dir():
        fail(f"{relative(BUNDLE / 'web')}: missing")

    for source_name, bundle_name in MIRRORED_FILES:
        check_mirrored_file(source_name, bundle_name)

    web_packages = web_directories = go_modules = 0
    if (BUNDLE / "web").is_dir():
        web_packages, web_directories = check_web_bundle()
    if (BUNDLE / "go").is_dir():
        go_modules = check_go_bundle()

    if problems:
        for problem in problems:
            print(f"error: {problem}", file=sys.stderr)
        print(
            f"dependency license bundle check failed: {len(problems)} problem(s).\n"
            "Regenerate with ./hack/generate-third-party-licenses.sh on a machine with registry "
            "access and commit dist/THIRD_PARTY_LICENSES, or dispatch the Release hygiene workflow "
            "on master with refresh=true.",
            file=sys.stderr,
        )
        return 1

    print(
        "Dependency license bundle is self-consistent "
        f"({go_modules} Go modules, {web_packages} web packages, {web_directories} web license directories)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))