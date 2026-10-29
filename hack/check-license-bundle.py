#!/usr/bin/env python3
"""Offline consistency gate for the committed third-party license bundle.

`hack/generate-third-party-licenses.sh` writes `dist/THIRD_PARTY_LICENSES/` from
the resolved Go module graph and the installed npm package tree. Regeneration
needs access to the Go module proxy and the npm registry, so the `Release
hygiene` workflow owns the registry-backed drift check: it regenerates the
bundle and fails when the committed copy would change.

This script is the registry-free companion gate that runs on every pull request
(`Build` -> `Validate`). It verifies that the committed bundle is consistent
with the dependency manifests that need no registry access, so a stale,
hand-edited or half-committed refresh is caught immediately instead of waiting
for the weekly regeneration job:

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
  * every module `go.mod` requires (direct or indirect) has a bundle entry for
    exactly that version, so a Go dependency bump without a bundle refresh
    fails the pull request
  * every `web/PACKAGE_LICENSES.tsv` row records the version
    `web/package-lock.json` resolves, and every lock package `npm ci` installs
    on the reference platform has a row, so a web dependency bump without a
    bundle refresh fails the pull request

What still needs the registry: equality with the full regenerated graph. The Go
bundle also carries the test-only modules that `go list -m all` pulls in beyond
the `go.mod` requires, upstream license texts can change without a version
bump, and platform-specific npm packages install per platform. The `Release
hygiene` workflow owns that authoritative comparison.

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
PACKAGE_LOCK = ROOT / "web" / "package-lock.json"

LICENSE_FILE_PATTERN = re.compile(r"^(licen[sc]e|copying|notice)", re.IGNORECASE)
SANITIZE_PATTERN = re.compile(r"[/:@+]")
GO_ENTRY_PATTERN = re.compile(r"^.+_v[0-9]")
APPROVED_BLOCK_PATTERN = re.compile(r"const approved = new Set\(\[(.*?)\]\)", re.DOTALL)
APPROVED_ENTRY_PATTERN = re.compile(r"['\"]([^'\"]+)['\"]")
GO_REQUIRE_BLOCK_PATTERN = re.compile(r"^\s*(\S+)\s+(v\S+)(?:\s*//.*)?$")
GO_REQUIRE_SINGLE_PATTERN = re.compile(r"^require\s+(\S+)\s+(v\S+)(?:\s*//.*)?$")
GO_MOD_BLOCK_KEYWORDS = {"require", "exclude", "replace", "retract"}

# npm ci installs optional packages only on platforms matching their os/cpu
# constraints. CI and the release images build on linux/amd64, so that is the
# reference platform for deciding which lock packages must appear in the bundle.
REFERENCE_OS = "linux"
REFERENCE_CPU = "x64"

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
    # The reviewed set is documented with whole-line `//` comments whose prose
    # may contain apostrophes (for example "Vite 8's rolldown toolchain"). Strip
    # those comment lines before extracting quoted entries, so comment prose can
    # never be paired with an entry's quotes and mistaken for an exception key.
    entry_lines = "\n".join(
        line for line in match.group(1).splitlines() if not line.strip().startswith("//")
    )
    return set(APPROVED_ENTRY_PATTERN.findall(entry_lines))


def check_web_bundle() -> tuple[list[list[str]], int]:
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
        return ([], 0)

    rows = read_rows(manifest_path, 3)
    approved_rows = read_rows(approved_path, 3)

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

    return (rows, len(actual_dirs))


def go_mod_requires() -> list[tuple[str, str]]:
    """Direct and indirect requires recorded in go.mod, in file order."""
    if not GO_MOD.is_file():
        fail(f"{relative(GO_MOD)}: missing; cannot compare the Go bundle against the module manifest")
        return []
    requires: list[tuple[str, str]] = []
    blocks: list[str] = []
    for number, line in enumerate(GO_MOD.read_text(encoding="utf-8").splitlines(), start=1):
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        keyword = stripped[:-1].strip() if stripped.endswith("(") else ""
        if keyword in GO_MOD_BLOCK_KEYWORDS:
            blocks.append(keyword)
            continue
        if stripped == ")" and blocks:
            blocks.pop()
            continue
        if blocks:
            if blocks[-1] != "require":
                continue
            match = GO_REQUIRE_BLOCK_PATTERN.match(line)
            if match is None:
                fail(f"{relative(GO_MOD)}:{number}: unparsed require entry {line!r}")
                continue
            requires.append((match.group(1), match.group(2)))
            continue
        match = GO_REQUIRE_SINGLE_PATTERN.match(stripped)
        if match is not None:
            requires.append((match.group(1), match.group(2)))
    return requires


def check_go_mod_versions(bundle_entries: set[str], missing_modules: set[str]) -> None:
    """Every go.mod require must resolve to a bundle entry of that version."""
    for module, version in go_mod_requires():
        if sanitize(f"{module}@{version}") in bundle_entries:
            continue
        if f"{module}@{version}" in missing_modules:
            continue
        stale = sorted(entry for entry in bundle_entries if entry.startswith(f"{sanitize(module)}_"))
        detail = f" (the bundle holds {', '.join(stale)})" if stale else ""
        fail(
            f"go.mod requires {module} {version} but dist/THIRD_PARTY_LICENSES/go has no license entry "
            f"for that version{detail}; regenerate the bundle with ./hack/generate-third-party-licenses.sh "
            "and commit it"
        )


def package_lock_packages() -> dict[str, dict]:
    """Top-level packages resolved by web/package-lock.json, keyed by name."""
    if not PACKAGE_LOCK.is_file():
        fail(f"{relative(PACKAGE_LOCK)}: missing; cannot compare the web bundle against the lockfile")
        return {}
    lock = json.loads(PACKAGE_LOCK.read_text(encoding="utf-8"))
    resolved: dict[str, dict] = {}
    for path, entry in lock.get("packages", {}).items():
        # The generator walks the top level of web/node_modules (plus @scope
        # groups), so nested node_modules trees are out of scope here too.
        if not isinstance(entry, dict) or path.count("node_modules/") != 1:
            continue
        if entry.get("link"):
            continue
        name = path.rsplit("node_modules/", 1)[1]
        if name and entry.get("version"):
            resolved[name] = entry
    return resolved


def lock_entry_installed_on_reference_platform(entry: dict) -> bool:
    """True when os/cpu constraints (if any) admit the reference platform."""
    for key, reference in (("os", REFERENCE_OS), ("cpu", REFERENCE_CPU)):
        constraint = entry.get(key)
        if isinstance(constraint, list) and reference not in constraint:
            return False
        if isinstance(constraint, str) and constraint != reference:
            return False
    return True


def check_web_lock_versions(rows: list[list[str]]) -> None:
    """PACKAGE_LICENSES.tsv must track the versions package-lock.json resolves."""
    lock = package_lock_packages()
    if not lock:
        return
    manifest: dict[str, str] = {}
    for name, version, _license in rows:
        manifest[name] = version

    for name, version in sorted(manifest.items()):
        entry = lock.get(name)
        if entry is None:
            fail(
                f"{WEB_MANIFEST}: records {name}@{version} but web/package-lock.json does not resolve "
                "it; regenerate the bundle with ./hack/generate-third-party-licenses.sh and commit it"
            )
        elif entry.get("version") != version:
            fail(
                f"{WEB_MANIFEST}: records {name}@{version} but web/package-lock.json resolves "
                f"{name}@{entry.get('version')}; regenerate the bundle with "
                "./hack/generate-third-party-licenses.sh and commit it"
            )

    for name in sorted(set(lock) - set(manifest)):
        entry = lock[name]
        if entry.get("optional") is True or not lock_entry_installed_on_reference_platform(entry):
            # Optional and platform-constrained packages install only on
            # matching platforms, so npm ci may legitimately leave them out of
            # node_modules and out of the generated manifest.
            continue
        fail(
            f"web/package-lock.json resolves {name}@{entry.get('version')} but {WEB_MANIFEST} has no "
            "row for it; regenerate the bundle with ./hack/generate-third-party-licenses.sh and "
            "commit it"
        )


def check_go_bundle() -> tuple[int, set[str], set[str]]:
    go_dir = BUNDLE / "go"
    entries = sorted(go_dir.iterdir())
    checked = 0
    bundle_entries: set[str] = set()
    for entry in entries:
        if entry.is_dir():
            if not GO_ENTRY_PATTERN.match(entry.name):
                fail(f"{relative(entry)}: directory name does not look like a sanitized module@version entry")
            check_license_directory(entry)
            checked += 1
            bundle_entries.add(entry.name)
        elif entry.name != "MISSING_LICENSE_FILES.tsv":
            fail(f"{relative(entry)}: unexpected file in the Go bundle; the generator only writes per-module license directories")
    missing_modules: set[str] = set()
    missing = go_dir / "MISSING_LICENSE_FILES.tsv"
    if missing.is_file():
        for module, version, note in read_rows(missing, 3):
            if note != "NO_LICENSE_FILE_FOUND":
                fail(f"{relative(missing)}: unexpected record {module}@{version} {note!r}")
            if sanitize(f"{module}@{version}") in bundle_entries:
                fail(f"{relative(go_dir / sanitize(module + '@' + version))}: has a license file but is listed in {missing.name}")
            missing_modules.add(f"{module}@{version}")
    return (checked, bundle_entries, missing_modules)


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

    web_rows: list[list[str]] = []
    web_directories = go_modules = 0
    if (BUNDLE / "web").is_dir():
        web_rows, web_directories = check_web_bundle()
        if web_rows:
            check_web_lock_versions(web_rows)
    if (BUNDLE / "go").is_dir():
        go_modules, go_entries, go_missing = check_go_bundle()
        check_go_mod_versions(go_entries, go_missing)

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
        "Dependency license bundle is self-consistent and matches the committed dependency "
        f"manifests ({go_modules} Go modules, {len(web_rows)} web packages, {web_directories} "
        "web license directories)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
