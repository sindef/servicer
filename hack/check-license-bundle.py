#!/usr/bin/env python3
"""Offline gate for the committed third-party license bundle.

`hack/generate-third-party-licenses.sh` writes `dist/THIRD_PARTY_LICENSES/` from
the resolved Go module graph and the installed npm package tree. Regeneration
needs access to the Go module proxy and the npm registry, so the `Release
hygiene` workflow owns the registry-backed drift check: it regenerates the
bundle and fails when the committed copy would change.

This script is the companion gate that runs on every pull request (`Build` ->
`Validate`) without registry access. It verifies that the committed bundle
matches the dependency graph it claims to license, so a dependency bump that
forgets to refresh the bundle fails on the pull request instead of in the
weekly job:

  * every module required by `go.mod` appears in the committed resolved module
    list `go/MODULES.tsv` at exactly the required version, and that list matches
    the module graph the local Go toolchain resolves from `go.mod` / `go.sum`
    when a warm module cache makes that possible (registry-free: `GOPROXY=off`)
  * every module in `go/MODULES.tsv` is recorded in `go.sum`
  * every `web/PACKAGE_LICENSES.tsv` row matches the version
    `web/package-lock.json` resolves for that package (platform-specific
    optional dependencies that the current platform does not install are
    allowed to be absent, but never at a wrong version)
  * the Go license directories and `go/MODULES.tsv` describe the same module
    set, so neither a stale directory nor a module without license material is
    left behind
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

What it still does not do: re-download license texts. Byte-for-byte license
contents are the `Release hygiene` regeneration comparison; this gate proves
the committed bundle describes the current dependency versions.

Usage:
    python3 hack/check-license-bundle.py [bundle-directory]
"""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DEFAULT_BUNDLE = ROOT / "dist" / "THIRD_PARTY_LICENSES"
GENERATOR = ROOT / "hack" / "generate-third-party-licenses.sh"
GO_MODFILE = ROOT / "go.mod"
GO_SUMFILE = ROOT / "go.sum"
WEB_LOCKFILE = ROOT / "web" / "package-lock.json"

LICENSE_FILE_PATTERN = re.compile(r"^(licen[sc]e|copying|notice)", re.IGNORECASE)
SANITIZE_PATTERN = re.compile(r"[/:@+]")
GO_ENTRY_PATTERN = re.compile(r"^.+_v[0-9]")
APPROVED_BLOCK_PATTERN = re.compile(r"const approved = new Set\(\[(.*?)\]\)", re.DOTALL)
APPROVED_ENTRY_PATTERN = re.compile(r"['\"]([^'\"]+)['\"]")
GO_REQUIRE_BLOCK = re.compile(r"^require\s*\($")
GO_REQUIRE_ENTRY = re.compile(r"^\s*(\S+)\s+(v\S+)\s*(?://.*)?$")
GO_REQUIRE_SINGLE = re.compile(r"^require\s+(\S+)\s+(v\S+)\s*(?://.*)?$")

WEB_MANIFEST = "web/PACKAGE_LICENSES.tsv"
WEB_APPROVED = "web/APPROVED_LICENSE_EXCEPTIONS.tsv"
WEB_MISSING = "web/MISSING_LICENSE_FILES.tsv"
GO_MANIFEST = "go/MODULES.tsv"
GO_MISSING = "go/MISSING_LICENSE_FILES.tsv"
ALLOWED_ROOT_ENTRIES = {"SERVICER-LICENSE", "THIRD_PARTY_NOTICES.md", "go", "web", "operators"}
MIRRORED_FILES = (
    ("LICENSE", "SERVICER-LICENSE"),
    ("THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.md"),
)

problems: list[str] = []
notices: list[str] = []


def fail(message: str) -> None:
    problems.append(message)


def notice(message: str) -> None:
    notices.append(message)


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

    Git cannot store an empty directory, so a directory without files is not
    part of the committed bundle and must not be treated as a bundle entry.
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
    """Module versions `go.mod` requires, direct and indirect."""
    if not GO_MODFILE.is_file():
        fail("go.mod: missing from the repository")
        return {}
    requirements: dict[str, str] = {}
    in_block = False
    for number, line in enumerate(GO_MODFILE.read_text(encoding="utf-8").splitlines(), start=1):
        if not in_block and GO_REQUIRE_BLOCK.match(line.strip()):
            in_block = True
            continue
        if in_block:
            if line.strip() == ")":
                in_block = False
                continue
            match = GO_REQUIRE_ENTRY.match(line)
        else:
            match = GO_REQUIRE_SINGLE.match(line.strip())
        if match is None:
            continue
        module, version = match.group(1), match.group(2)
        previous = requirements.get(module)
        if previous is not None and previous != version:
            fail(f"go.mod:{number}: {module} is required at both {previous} and {version}")
            continue
        requirements[module] = version
    if not requirements:
        fail("go.mod: could not read any require entries")
    return requirements


def resolved_go_modules() -> dict[str, str] | None:
    """Module graph the local toolchain resolves from go.mod and go.sum.

    Returns None when the toolchain is unavailable or the module cache is cold:
    `GOPROXY=off` keeps the gate registry-free, and the go.mod comparison still
    runs in that case.
    """
    if shutil.which("go") is None:
        return None
    environment = dict(os.environ)
    environment.update({"GOFLAGS": "-mod=readonly", "GOPROXY": "off", "GOTOOLCHAIN": "local"})
    try:
        completed = subprocess.run(
            ["go", "list", "-m", "-f", "{{if not .Main}}{{.Path}}\t{{.Version}}{{end}}", "all"],
            cwd=ROOT,
            capture_output=True,
            text=True,
            timeout=300,
            env=environment,
        )
    except (OSError, subprocess.SubprocessError):
        return None
    if completed.returncode != 0:
        return None
    modules: dict[str, str] = {}
    for line in completed.stdout.splitlines():
        if line.strip() == "":
            continue
        module, _, version = line.partition("\t")
        modules[module] = version
    return modules or None


def go_sum_entries() -> set[tuple[str, str]]:
    if not GO_SUMFILE.is_file():
        fail("go.sum: missing from the repository")
        return set()
    entries: set[tuple[str, str]] = set()
    for line in GO_SUMFILE.read_text(encoding="utf-8").splitlines():
        fields = line.split()
        if len(fields) < 2:
            continue
        module = fields[0]
        version = fields[1].removesuffix("/go.mod")
        entries.add((module, version))
    return entries


def read_go_manifest() -> dict[str, str]:
    path = BUNDLE / GO_MANIFEST
    if not path.is_file():
        fail(
            f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: missing; hack/generate-third-party-licenses.sh "
            "commits the resolved module list so this gate can diff it against go.mod"
        )
        return {}
    modules: dict[str, str] = {}
    for module, version in read_rows(path, 2):
        if module in modules:
            fail(f"{relative(path)}: duplicate entry for {module}")
            continue
        modules[module] = version
    if not modules:
        fail(f"{relative(path)}: no module entries; refresh the bundle")
    return modules


def check_go_modules_against_go_mod(modules: dict[str, str]) -> None:
    requirements = go_mod_requirements()
    for module in sorted(requirements):
        required = requirements[module]
        recorded = modules.get(module)
        if recorded is None:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: missing {module} {required} required by go.mod; "
                "the committed bundle is older than the dependency graph it claims to license"
            )
        elif recorded != required:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: records {module} {recorded} but go.mod requires "
                f"{required}; refresh dist/THIRD_PARTY_LICENSES"
            )


def check_go_modules_against_go_sum(modules: dict[str, str]) -> None:
    if not modules:
        return
    entries = go_sum_entries()
    for module in sorted(modules):
        version = modules[module]
        if (module, version) not in entries:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: {module} {version} is not recorded in go.sum; "
                "the committed module list is stale or hand-edited"
            )


def check_go_modules_against_toolchain(modules: dict[str, str]) -> None:
    if not modules:
        return
    resolved = resolved_go_modules()
    if resolved is None:
        notice(
            "go toolchain could not resolve the module graph offline (cold module cache); "
            "compared the bundle against go.mod requirements only"
        )
        return
    for module in sorted(resolved):
        version = resolved[module]
        recorded = modules.get(module)
        if recorded is None:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: missing {module} {version} from the resolved module "
                "graph; refresh dist/THIRD_PARTY_LICENSES"
            )
        elif recorded != version:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: records {module} {recorded} but the resolved module "
                f"graph selects {version}; refresh dist/THIRD_PARTY_LICENSES"
            )
    for module in sorted(modules):
        if module not in resolved:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{GO_MANIFEST}: records {module} {modules[module]} which the resolved "
                "module graph no longer contains; refresh dist/THIRD_PARTY_LICENSES"
            )


def lockfile_packages() -> dict[str, tuple[str, bool]] | None:
    """Top-level packages `web/package-lock.json` resolves, with their optional flag."""
    if not WEB_LOCKFILE.is_file():
        fail("web/package-lock.json: missing from the repository")
        return None
    try:
        lock = json.loads(WEB_LOCKFILE.read_text(encoding="utf-8"))
    except json.JSONDecodeError as error:
        fail(f"web/package-lock.json: cannot parse: {error}")
        return None
    entries = lock.get("packages")
    if not isinstance(entries, dict):
        fail("web/package-lock.json: no `packages` map; regenerate the lockfile with npm")
        return None
    packages: dict[str, tuple[str, bool]] = {}
    for path, entry in sorted(entries.items()):
        if path == "" or not path.startswith("node_modules/"):
            continue
        if "/node_modules/" in path:
            # Nested copy; the generator records the top-level install only.
            continue
        if not isinstance(entry, dict) or entry.get("link"):
            continue
        name = path[len("node_modules/"):]
        version = entry.get("version")
        if not isinstance(version, str) or version == "":
            fail(f"web/package-lock.json: {path} has no resolved version")
            continue
        if name in packages and packages[name][0] != version:
            fail(f"web/package-lock.json: {name} resolves to both {packages[name][0]} and {version}")
            continue
        packages[name] = (version, bool(entry.get("optional")))
    if not packages:
        fail("web/package-lock.json: no resolved packages")
    return packages


def check_web_manifest_against_lockfile(rows: list[list[str]]) -> None:
    resolved = lockfile_packages()
    if resolved is None:
        return
    listed = {name: version for name, version, _license in rows}
    for name in sorted(resolved):
        version, optional = resolved[name]
        recorded = listed.get(name)
        if recorded is None:
            if optional:
                # Platform-specific binding the current platform does not install.
                continue
            fail(
                f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: missing {name} {version} pinned by "
                "web/package-lock.json; the committed bundle is older than the dependency graph it claims to license"
            )
        elif recorded != version:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: records {name} {recorded} but "
                f"web/package-lock.json resolves {version}; refresh dist/THIRD_PARTY_LICENSES"
            )
    for name in sorted(listed):
        if name not in resolved:
            fail(
                f"dist/THIRD_PARTY_LICENSES/{WEB_MANIFEST}: records {name} {listed[name]} which "
                "web/package-lock.json no longer contains; refresh dist/THIRD_PARTY_LICENSES"
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

    check_web_manifest_against_lockfile(rows)

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


def check_go_bundle(modules: dict[str, str]) -> int:
    go_dir = BUNDLE / "go"
    entries = sorted(go_dir.iterdir())
    directories = populated_directories(go_dir)
    checked = 0
    for entry in entries:
        if entry.is_dir():
            if not license_files(entry):
                # Empty directory: impossible to commit, ignore a stale checkout.
                continue
            if not GO_ENTRY_PATTERN.match(entry.name):
                fail(f"{relative(entry)}: directory name does not look like a sanitized module@version entry")
            check_license_directory(entry)
            checked += 1
        elif entry.name not in {Path(GO_MANIFEST).name, Path(GO_MISSING).name}:
            fail(
                f"{relative(entry)}: unexpected file in the Go bundle; the generator only writes per-module "
                "license directories and the resolved module list"
            )

    missing_path = go_dir / Path(GO_MISSING).name
    missing_modules: set[str] = set()
    if missing_path.is_file():
        for module, version, note in read_rows(missing_path, 3):
            if note != "NO_LICENSE_FILE_FOUND":
                fail(f"{relative(missing_path)}: unexpected record {module}@{version} {note!r}")
            key = f"{module}@{version}"
            missing_modules.add(key)
            if sanitize(key) in {entry.name for entry in directories}:
                fail(f"{relative(go_dir / sanitize(key))}: has a license file but is listed in {missing_path.name}")
            if modules and module not in modules:
                fail(f"{relative(missing_path)}: {key} is not in {GO_MANIFEST}; refresh the bundle")

    for module in sorted(modules):
        key = f"{module}@{modules[module]}"
        if sanitize(key) not in {entry.name for entry in directories} and key not in missing_modules:
            fail(
                f"{relative(go_dir / sanitize(key))}: missing license directory for {key} and no entry in "
                f"{missing_path.name}; refresh the bundle"
            )

    for directory in directories:
        owner = None
        for module in modules:
            if sanitize(f"{module}@{modules[module]}") == directory.name:
                owner = module
                break
        if owner is None:
            fail(
                f"{relative(directory)}: no {GO_MANIFEST} row; regenerate the bundle instead of adding directories"
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
    if (BUNDLE / "go").is_dir():
        modules = read_go_manifest()
        check_go_modules_against_go_mod(modules)
        check_go_modules_against_go_sum(modules)
        check_go_modules_against_toolchain(modules)
        go_modules = check_go_bundle(modules)
    if (BUNDLE / "web").is_dir():
        web_packages, web_directories = check_web_bundle()

    for message in notices:
        print(f"notice: {message}", file=sys.stderr)

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
        "Dependency license bundle matches the resolved dependency versions "
        f"({go_modules} Go modules, {web_packages} web packages, {web_directories} web license directories)."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
