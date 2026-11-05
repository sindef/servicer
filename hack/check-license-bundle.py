#!/usr/bin/env python3
"""Offline drift + self-consistency gate for the committed third-party license bundle.

`hack/generate-third-party-licenses.sh` writes `dist/THIRD_PARTY_LICENSES/` from
the resolved Go module graph and the installed npm package tree. Regeneration
needs access to the Go module proxy and the npm registry, so the `Release
hygiene` workflow owns the registry-backed regeneration diff: it regenerates
the bundle and fails when the committed copy would change.

This script is the registry-free companion gate that runs on every pull request
(`Build` -> `Validate`). It compares the committed bundle against the dependency
manifests that are committed in this repository, so a dependency bump that
forgets to refresh the bundle fails on the pull request instead of waiting for
the weekly regeneration job:

  * `go/MODULES.tsv` records the resolved module graph the bundle was built
    from; it must match the license directories one to one, and every `require`
    in `go.mod` must appear there at the same version
  * every top-level package in `web/package-lock.json` must appear in
    `web/PACKAGE_LICENSES.tsv` at the same version, and every row must still
    exist in the lockfile (platform-specific optional packages may be absent,
    because only the packages installed for the generating platform are bundled)
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
GO_MANIFEST = "go/MODULES.tsv"
GO_MISSING = "go/MISSING_LICENSE_FILES.tsv"
GO_BUNDLE_FILES = {GO_MISSING, "MODULES.tsv"}
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


def has_files(directory: Path) -> bool:
    """True when a directory holds committed content.

    Git cannot store empty directories, but a working tree can keep the empty
    parent (or empty nested parents) of license files that a bundle refresh
    deleted. Only directories that still hold a file belong to the bundle.
    """
    return any(path.is_file() for path in directory.rglob("*"))


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
    """Module versions `go.mod` requires, read without touching the module proxy."""
    if not GO_MOD.is_file():
        fail("go.mod: missing from the repository; cannot compare the bundle against it")
        return {}
    requirements: dict[str, str] = {}
    in_block = False
    for raw in GO_MOD.read_text(encoding="utf-8").splitlines():
        line = raw.split("//", 1)[0].strip()
        if not line:
            continue
        if in_block:
            if line == ")":
                in_block = False
                continue
            fields = line.split()
            if len(fields) >= 2 and fields[1].startswith("v"):
                requirements[fields[0]] = fields[1]
            continue
        if line.startswith("require ("):
            in_block = True
            continue
        if line.startswith("require "):
            fields = line[len("require ") :].split()
            if len(fields) >= 2 and fields[1].startswith("v"):
                requirements[fields[0]] = fields[1]
    return requirements


def package_lock_versions() -> dict[str, tuple[str, bool]] | None:
    """Top-level resolved npm packages from web/package-lock.json.

    Nested copies (`node_modules/a/node_modules/b`) are skipped: the generator
    records one row and one license directory per resolved name@version.
    """
    if not WEB_LOCK.is_file():
        fail("web/package-lock.json: missing from the repository; cannot compare the bundle against it")
        return None
    try:
        lock = json.loads(WEB_LOCK.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        fail(f"web/package-lock.json: cannot be parsed ({error})")
        return None
    packages = lock.get("packages")
    if not isinstance(packages, dict):
        fail("web/package-lock.json: no `packages` map; the lockfile format changed, update this gate")
        return None
    resolved: dict[str, tuple[str, bool]] = {}
    for key, meta in packages.items():
        if not isinstance(meta, dict) or not key.startswith("node_modules/"):
            continue
        name = key[len("node_modules/") :]
        if "node_modules/" in name:
            continue
        version = meta.get("version")
        if not isinstance(version, str) or not version:
            continue
        resolved[name] = (version, bool(meta.get("optional")))
    return resolved


def go_module_manifest() -> dict[str, str] | None:
    path = BUNDLE / GO_MANIFEST
    if not path.is_file():
        fail(
            f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: missing; the generator records the resolved module "
            "graph there so pull requests can detect a stale bundle without registry access"
        )
        return None
    modules: dict[str, str] = {}
    for module, version in read_rows(path, 2):
        if module in modules:
            fail(f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: duplicate entry for {module}")
        modules[module] = version
    return modules


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

    row_keys: list[str] = []
    expected_dirs: dict[str, str] = {}
    for name, version, _license in rows:
        key = f"{name}@{version}"
        if key in row_keys:
            fail(f"{WEB_MANIFEST}: duplicate entry for {key}")
        row_keys.append(key)
        expected_dirs[sanitize(key)] = key

    # Git cannot store empty directories. Ignore them so the gate reflects the
    # committed bundle when a working tree still holds the empty parent of a
    # license file that a refresh deleted.
    actual_dirs = sorted(
        path for path in (BUNDLE / "web").iterdir() if path.is_dir() and has_files(path)
    )
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

    resolved = package_lock_versions()
    if resolved is not None:
        row_key_set = set(row_keys)
        for name, (version, optional) in sorted(resolved.items()):
            key = f"{name}@{version}"
            if key in row_key_set:
                continue
            if optional:
                # Platform-specific optional package: the bundle records only the
                # copies installed on the generating platform.
                continue
            fail(
                f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: web/package-lock.json resolves {key} but the bundle "
                "has no row for it; regenerate the bundle"
            )
        for name, version, _license in rows:
            key = f"{name}@{version}"
            entry = resolved.get(name)
            if entry is None:
                fail(
                    f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: row {key} is not a top-level package in "
                    "web/package-lock.json; regenerate the bundle instead of editing the manifest"
                )
            elif entry[0] != version:
                fail(
                    f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: row {key} disagrees with web/package-lock.json, "
                    f"which resolves {name} {entry[0]}; regenerate the bundle"
                )

    return (len(row_keys), len(actual_dirs))


def check_go_bundle() -> int:
    go_dir = BUNDLE / "go"
    entries = sorted(go_dir.iterdir())
    # Git cannot store empty directories: a working tree may keep the empty
    # parent of a license file that a refresh deleted. Only directories with
    # committed content are part of the bundle.
    directories = {
        entry.name: entry for entry in entries if entry.is_dir() and has_files(entry)
    }

    modules = go_module_manifest()
    expected: dict[str, str] = {}
    if modules is not None:
        for module, version in modules.items():
            key = f"{module}@{version}"
            expected[sanitize(key)] = key

    missing_rows: dict[str, str] = {}
    missing_path = go_dir / "MISSING_LICENSE_FILES.tsv"
    if missing_path.is_file():
        for module, version, note in read_rows(missing_path, 3):
            if note != "NO_LICENSE_FILE_FOUND":
                fail(f"{relative(missing_path)}: unexpected record {module}@{version} {note!r}")
            key = f"{module}@{version}"
            missing_rows[sanitize(key)] = key
            if sanitize(key) in directories:
                fail(f"{relative(go_dir / sanitize(key))}: has a license file but is listed in {missing_path.name}")

    checked = 0
    for name, entry in sorted(directories.items()):
        if not GO_ENTRY_PATTERN.match(name):
            fail(f"{relative(entry)}: directory name does not look like a sanitized module@version entry")
        elif modules is not None and name not in expected and name not in missing_rows:
            fail(
                f"{relative(entry)}: no {GO_MANIFEST} row; the bundle holds a module version that is no longer "
                "resolved; regenerate the bundle instead of leaving stale license directories"
            )
        check_license_directory(entry)
        checked += 1

    for entry in entries:
        if entry.is_file() and entry.name not in GO_BUNDLE_FILES:
            fail(f"{relative(entry)}: unexpected file in the Go bundle; the generator only writes per-module license directories")

    for key, label in sorted(expected.items()):
        if key not in directories and key not in missing_rows:
            fail(
                f"{relative(go_dir / key)}: missing license directory for {label}; regenerate the bundle"
            )
    for key, label in sorted(missing_rows.items()):
        if modules is not None and key not in expected:
            fail(f"{relative(missing_path)}: {label} is no longer part of the resolved module graph")

    if modules is not None:
        for module, version in sorted(go_mod_requirements().items()):
            recorded = modules.get(module)
            if recorded is None:
                fail(
                    f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: go.mod requires {module} {version} but the bundle "
                    "has no entry for that module; regenerate the bundle"
                )
            elif recorded != version:
                fail(
                    f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: go.mod requires {module} {version} but the bundle "
                    f"records {module} {recorded}; regenerate the bundle"
                )

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
        "Dependency license bundle matches go.mod and web/package-lock.json "
        f"({go_modules} Go modules, {web_packages} web packages, {web_directories} web license directories)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
