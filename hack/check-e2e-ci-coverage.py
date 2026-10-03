#!/usr/bin/env python3
"""Offline gate: every e2e suite is wired into CI and gates releases.

The repository ships several KinD end-to-end suites under hack/e2e-*.sh. A suite
that no workflow invokes is dead weight: it never detects a regression, and a
release can ship without having run it. This check fails when that drift comes
back.

Checks against the working tree:
  1. Every hack/e2e-*.sh script is invoked by a run step of at least one
     workflow file (a mention in a comment does not count).
  2. Every .github/workflows/e2e-*.yml workflow runs its own suite script and is
     called as a reusable workflow from .github/workflows/build.yml, which is
     what makes it available to the release jobs.
  3. The tag-release jobs of build.yml (publish-install-manifest and
     release-security) list every one of those e2e callers in their needs, so a
     v* tag cannot publish a release without a green e2e run.

Only stdlib is used because build.yml runs this on the default runner image
without installing anything.
"""

from __future__ import annotations

import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
WORKFLOW_DIR = REPO_ROOT / ".github" / "workflows"
BUILD_WORKFLOW = WORKFLOW_DIR / "build.yml"

# Jobs in build.yml that publish a release. Renaming one of them means updating
# this list, which is intentional: the release gate must stay explicit.
RELEASE_JOBS = ("publish-install-manifest", "release-security")

E2E_WORKFLOW_PREFIX = "e2e-"


def parse_jobs(text: str) -> dict[str, dict[str, object]]:
    """Return {job id: {"needs": [...], "uses": str | None}} for a workflow.

    A deliberately small indentation reader instead of a YAML dependency: this
    runs before any dependency is installed.
    """
    jobs: dict[str, dict[str, object]] = {}
    job_id: str | None = None
    section: str | None = None
    in_jobs = False

    for raw in text.splitlines():
        line = raw.rstrip()
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        indent = len(line) - len(line.lstrip(" "))
        if not in_jobs:
            if indent == 0 and stripped == "jobs:":
                in_jobs = True
            continue
        if indent == 0:
            in_jobs = False
            job_id = None
            section = None
            continue
        if indent == 2 and stripped.endswith(":") and not stripped.startswith("- "):
            job_id = stripped[:-1].strip()
            jobs[job_id] = {"needs": [], "uses": None}
            section = None
            continue
        if job_id is None:
            continue
        if indent == 4:
            section = None
            if stripped.startswith("needs:"):
                inline = stripped[len("needs:") :].strip()
                if inline.startswith("["):
                    for entry in inline.strip("[]").split(","):
                        entry = entry.strip().strip("'\"")
                        if entry:
                            jobs[job_id]["needs"].append(entry)  # type: ignore[union-attr]
                else:
                    section = "needs"
            elif stripped.startswith("uses:"):
                jobs[job_id]["uses"] = stripped[len("uses:") :].strip().strip("'\"")
            continue
        if section == "needs" and indent >= 6 and stripped.startswith("- "):
            jobs[job_id]["needs"].append(stripped[2:].strip().strip("'\""))  # type: ignore[union-attr]

    return jobs


def run_command_lines(text: str) -> list[str]:
    """Return the lines a workflow actually executes, including run block scalars.

    A comment that mentions a script path must not satisfy the wiring check, so
    only run steps count.
    """
    lines = text.splitlines()
    commands: list[str] = []
    index = 0
    while index < len(lines):
        line = lines[index]
        stripped = line.strip()
        if not stripped.startswith("run:"):
            index += 1
            continue
        value = stripped[len("run:") :].strip()
        if value and value not in ("|", "|-", ">", ">-"):
            commands.append(line)
            index += 1
            continue
        indent = len(line) - len(line.lstrip(" "))
        index += 1
        while index < len(lines):
            following = lines[index]
            if not following.strip():
                index += 1
                continue
            if len(following) - len(following.lstrip(" ")) <= indent:
                break
            commands.append(following)
            index += 1
    return commands


def main() -> int:
    failures: list[str] = []

    scripts = sorted(p.name for p in (REPO_ROOT / "hack").glob("e2e-*.sh"))
    if not scripts:
        failures.append("hack/e2e-*.sh: no end-to-end suites found")

    workflow_texts = {
        path.name: path.read_text(encoding="utf-8")
        for path in sorted(WORKFLOW_DIR.glob("*.yml"))
    }
    if not workflow_texts:
        failures.append(f"{WORKFLOW_DIR}: no workflow files found")

    workflow_commands = {
        name: run_command_lines(text) for name, text in workflow_texts.items()
    }

    # 1. Every suite script is executed by some workflow step.
    for script in scripts:
        invocation = f"hack/{script}"
        if not any(
            invocation in line
            for commands in workflow_commands.values()
            for line in commands
        ):
            failures.append(
                f"no workflow step runs {invocation}; add it to a workflow or "
                "remove the suite"
            )

    # 2. Every e2e workflow is callable from build.yml.
    e2e_workflows = sorted(
        name for name in workflow_texts if name.startswith(E2E_WORKFLOW_PREFIX)
    )
    if not e2e_workflows:
        failures.append(
            f"{WORKFLOW_DIR}: no {E2E_WORKFLOW_PREFIX}*.yml workflow found to gate releases"
        )

    build_text = workflow_texts.get(BUILD_WORKFLOW.name)
    if build_text is None:
        failures.append(f"{BUILD_WORKFLOW} is missing")
        build_jobs: dict[str, dict[str, object]] = {}
    else:
        build_jobs = parse_jobs(build_text)

    gate_jobs: dict[str, str] = {}
    for name in e2e_workflows:
        reference = f"./.github/workflows/{name}"
        callers = [
            job_id
            for job_id, job in build_jobs.items()
            if job.get("uses") == reference
        ]
        if not callers:
            failures.append(
                f"{BUILD_WORKFLOW.name}: no job calls {reference}, so the release "
                "jobs cannot depend on that suite"
            )
            continue
        for caller in callers:
            gate_jobs[caller] = name
        suite_script = f"hack/{name[:-4]}.sh"
        if not any(suite_script in line for line in workflow_commands[name]):
            failures.append(f"{name}: no run step executes {suite_script}")

    # 3. The release jobs depend on every e2e gate.
    for job_id in RELEASE_JOBS:
        job = build_jobs.get(job_id)
        if job is None:
            failures.append(
                f"{BUILD_WORKFLOW.name}: release job {job_id} is missing; update "
                "RELEASE_JOBS in this check if it was renamed"
            )
            continue
        needs = set(job.get("needs") or [])
        missing = sorted(set(gate_jobs) - needs)
        if missing:
            failures.append(
                f"{BUILD_WORKFLOW.name}: release job {job_id} does not need "
                f"{', '.join(missing)}, so a tag could publish without a green e2e run"
            )

    if failures:
        print("e2e CI coverage check failed:")
        for failure in failures:
            print(f"  - {failure}")
        return 1

    print(
        "e2e CI coverage ok: "
        f"{len(scripts)} suite script(s) wired, "
        f"{len(e2e_workflows)} workflow(s) called from {BUILD_WORKFLOW.name}, "
        f"{', '.join(sorted(gate_jobs))} gate "
        f"{', '.join(RELEASE_JOBS)}."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
