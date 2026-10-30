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
  * empty directories are ignored: git cannot store them, so they only exist in
    a working tree where a refresh already deleted the stale license files
  * manifests are sorted, unique, and free of malformed rows
  * no `web/MISSING_LICENSE_FILES.tsv`, which would mean the bundle was
    committed with unapproved missing license files

It also compares the manifest versions against the dependency manifests that
are committed next to them, without needing registry access:

  * every module required by `go.mod` has a license directory for the required
    version, so a stale `k8s.io_api_v0.32.1` directory behind a
    `k8s.io/api v0.37.1` requirement fails here instead of waiting for the
    weekly regeneration job
  * every `web/PACKAGE_LICENSES.tsv` row matches the version
    `web/package-lock.json` resolves for that package name, and no row survives
    for a package the lockfile no longer resolves

What it deliberately does not do: reproduce the generator output file by file.
License texts, notice files and the set of installed npm packages still need a
machine with module proxy and registry access, which is the `Release hygiene`
workflow's regeneration-and-diff job.

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


def populated_directories(directory: Path) -> list[Path]:
    """Subdirectories that hold at least one file.

    Git cannot store empty directories, so an empty license directory can only
    appear in a working tree, never in a committed bundle; ignoring it keeps the
    check meaningful both on a checkout and right after a local refresh.
    """
    return sorted(path for path in directory.iterdir() if path.is_dir() and license_files(path))


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


def go_mod_requirements() -> dict[str, str]:
    """Module versions required by the committed `go.mod`."""
    if not GO_MOD.is_file():
        fail("go.mod: missing from the repository")
        return {}
    requirements: dict[str, str] = {}
    in_block = False
    for line in GO_MOD.read_text(encoding="utf-8").splitlines():
        stripped = line.split("//")[0].strip()
        if stripped == "":
            continue
        if stripped.startswith("require ("):
            in_block = True
            continue
        if in_block and stripped == ")":
            in_block = False
            continue
        if stripped.startswith("require "):
            entry = stripped[len("require ") :].strip()
        elif in_block:
            entry = stripped
        else:
            continue
        fields = entry.split()
        if len(fields) >= 2 and fields[1].startswith("v"):
            requirements[fields[0]] = fields[1]
    return requirements


def web_lock_versions() -> dict[str, str]:
    """Package versions resolved by `web/package-lock.json` (top-level installs)."""
    if not WEB_LOCK.is_file():
        fail("web/package-lock.json: missing from the repository")
        return {}
    lock = json.loads(WEB_LOCK.read_text(encoding="utf-8"))
    resolved: dict[str, str] = {}
    for key, entry in lock.get("packages", {}).items():
        if not key.startswith("node_modules/"):
            continue
        name = key[len("node_modules/") :]
        if "node_modules/" in name:
            # Nested duplicate; the generator reports the installed copy.
            continue
        version = entry.get("version")
        if isinstance(version, str) and version:
            resolved[name] = version
    return resolved


def check_go_version_drift(populated_names: set[str]) -> None:
    requirements = go_mod_requirements()
    available: dict[str, set[str]] = {}
    for name in populated_names:
        module, _sep, version = name.rpartition("_v")
        if module:
            available.setdefault(module, set()).add("v" + version)
    for module, version in sorted(requirements.items()):
        if sanitize(f"{module}@{version}") in populated_names:
            continue
        stale = sorted(available.get(sanitize(module), set()))
        detail = f"; the bundle only has {', '.join(stale)}" if stale else ""
        fail(
            f"dist/THIRD_PARTY_LICENSES/go/{sanitize(module + '@' + version)}: missing license directory for "
            f"{module} {version} required by go.mod{detail}. Regenerate the bundle with "
            "hack/generate-third-party-licenses.sh"
        )


def check_web_version_drift(rows: list[list[str]]) -> None:
    resolved = web_lock_versions()
    for name, version, _license in rows:
        expected = resolved.get(name)
        if expected is None:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: row for {name} {version} has no matching package in "
                "web/package-lock.json; regenerate the bundle instead of editing it"
            )
        elif expected != version:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: {name} is listed as {version} but "
                f"web/package-lock.json resolves {expected}; regenerate the bundle"
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
    check_web_version_drift(rows)

    row_keys: list[str] = []
    expected_dirs: dict[str, str] = {}
    for name, version, _license in rows:
        key = f"{name}@{version}"
        if key in row_keys:
            fail(f"{WEB_MANIFEST}: duplicate entry for {key}")
        row_keys.append(key)
        expected_dirs[sanitize(key)] = key

    actual_dirs = populated_directories(BUNDLE / "web")
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
    populated = populated_directories(go_dir)
    populated_names = {entry.name for entry in populated}
    checked = 0
    for entry in sorted(go_dir.iterdir()):
        if not entry.is_dir():
            if entry.name != "MISSING_LICENSE_FILES.tsv":
                fail(f"{relative(entry)}: unexpected file in the Go bundle; the generator only writes per-module license directories")
            continue
        if entry.name not in populated_names:
            # Empty directory left behind by a refresh; not committable.
            continue
        if not GO_ENTRY_PATTERN.match(entry.name):
            fail(f"{relative(entry)}: directory name does not look like a sanitized module@version entry")
        check_license_directory(entry)
        checked += 1
    missing = go_dir / "MISSING_LICENSE_FILES.tsv"
    if missing.is_file():
        for module, version, note in read_rows(missing, 3):
            if note != "NO_LICENSE_FILE_FOUND":
                fail(f"{relative(missing)}: unexpected record {module}@{version} {note!r}")
            if sanitize(f"{module}@{version}") in populated_names:
                fail(f"{relative(go_dir / sanitize(module + '@' + version))}: has a license file but is listed in {missing.name}")
    check_go_version_drift(populated_names)
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