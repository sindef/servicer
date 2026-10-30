#!/usr/bin/env python3
"""Offline gate: every KinD e2e script is wired into CI and gates tag releases.

The suite scripts under hack/e2e-*.sh only protect a release when a workflow
invokes them and when the tag release path waits for that run. This gate fails
when:

* a hack/e2e-*.sh script is not invoked by any `.github/workflows/*.yml` step;
* the workflow that invokes it cannot be reused as a release gate because it is
  missing a `workflow_call` trigger;
* `.github/workflows/build.yml` does not trigger on `v*` tags, does not call
  that workflow from a job conditioned on a tag push, or lets
  `publish-install-manifest` / `release-security` run without needing that job.

Run it from the repository root: `python3 hack/check-e2e-workflows.py`.
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

TAG_RELEASE_JOBS = ("publish-install-manifest", "release-security")
BUILD_WORKFLOW = "build.yml"


def e2e_scripts(root: Path) -> list[str]:
    return sorted(path.name for path in (root / "hack").glob("e2e-*.sh"))


def workflows(root: Path) -> dict[str, str]:
    directory = root / ".github" / "workflows"
    return {path.name: path.read_text(encoding="utf-8") for path in sorted(directory.glob("*.yml"))}


def invoked_script(text: str) -> list[str]:
    """Scripts named on a workflow `run:` line, not only mentioned in comments."""
    found = []
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("#") or not stripped.startswith("run:"):
            continue
        found.extend(re.findall(r"hack/(e2e-[A-Za-z0-9._-]+\.sh)", stripped))
    return found


def job_blocks(build_text: str) -> dict[str, str]:
    """Split the `jobs:` mapping of build.yml into one block per job id."""
    lines = build_text.splitlines()
    if "jobs:" not in lines:
        return {}
    blocks: dict[str, list[str]] = {}
    current = None
    for line in lines[lines.index("jobs:") + 1 :]:
        if line and not line.startswith(" "):
            break  # another top-level key ends the jobs mapping
        match = re.match(r"^  ([A-Za-z0-9_-]+):\s*$", line)
        if match:
            current = match.group(1)
            blocks[current] = []
            continue
        if current is not None:
            blocks[current].append(line)
    return {name: "\n".join(body) for name, body in blocks.items()}


def job_needs(block: str) -> list[str]:
    inline = re.search(r"^    needs:\s*\[(.*?)\]\s*$", block, re.MULTILINE)
    if inline:
        return [item.strip().strip("'\"") for item in inline.group(1).split(",") if item.strip()]
    listed = re.search(r"^    needs:\s*$\n((?: {6}- .*\n?)+)", block, re.MULTILINE)
    if not listed:
        return []
    return [
        line.strip()[1:].strip().strip("'\"")
        for line in listed.group(1).splitlines()
        if line.strip().startswith("- ")
    ]


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--repo-root",
        default=str(Path(__file__).resolve().parent.parent),
        help="repository root to check (default: the parent of hack/)",
    )
    args = parser.parse_args(argv)
    root = Path(args.repo_root).resolve()

    errors: list[str] = []
    scripts = e2e_scripts(root)
    if not scripts:
        errors.append(f"no hack/e2e-*.sh scripts found under {root}")
        return report(errors)

    files = workflows(root)
    if BUILD_WORKFLOW not in files:
        errors.append(f".github/workflows/{BUILD_WORKFLOW} is missing")
        return report(errors)

    build_text = files[BUILD_WORKFLOW]
    build_jobs = job_blocks(build_text)
    if not re.search(r"^    tags:\s*\[.*v\*.*\]\s*$", build_text, re.MULTILINE):
        errors.append(f"{BUILD_WORKFLOW} does not trigger on `v*` tags")

    # Workflow file name -> job id in build.yml that calls it for a tag push.
    release_gate_jobs: dict[str, str] = {}
    for job_id, block in build_jobs.items():
        match = re.search(r"^    uses:\s*\./\.github/workflows/([A-Za-z0-9._-]+)\s*$", block, re.MULTILINE)
        if not match:
            continue
        if "refs/tags/v" not in block:
            errors.append(
                f"{BUILD_WORKFLOW} job `{job_id}` calls {match.group(1)} without a `refs/tags/v` condition"
            )
            continue
        release_gate_jobs[match.group(1)] = job_id

    for script in scripts:
        callers = {
            name: text
            for name, text in files.items()
            if script in invoked_script(text)
        }
        if not callers:
            errors.append(f"hack/{script} is not invoked by any .github/workflows/*.yml step")
            continue
        for workflow_name, text in sorted(callers.items()):
            if not re.search(r"^  workflow_call:", text, re.MULTILINE):
                errors.append(
                    f"{workflow_name} runs hack/{script} but has no `workflow_call` trigger, "
                    "so the tag release path cannot reuse it"
                )
                continue
            gate_job = release_gate_jobs.get(workflow_name)
            if gate_job is None:
                errors.append(
                    f"hack/{script} runs in {workflow_name}, but {BUILD_WORKFLOW} never calls "
                    f"{workflow_name} from a `v*` tag job"
                )

    gate_jobs = set(release_gate_jobs.values())
    for job_id in TAG_RELEASE_JOBS:
        block = build_jobs.get(job_id)
        if block is None:
            errors.append(f"{BUILD_WORKFLOW} has no `{job_id}` job")
            continue
        missing = sorted(gate_jobs - set(job_needs(block)))
        if missing:
            errors.append(
                f"{BUILD_WORKFLOW} job `{job_id}` can publish without the e2e gate; "
                f"add these to its needs: {', '.join(missing)}"
            )

    if not errors:
        print(
            "e2e workflow coverage OK: "
            + ", ".join(f"hack/{script}" for script in scripts)
            + f" gated by {', '.join(sorted(gate_jobs))}"
        )
    return report(errors)


def report(errors: list[str]) -> int:
    if not errors:
        return 0
    for error in errors:
        print(f"error: {error}", file=sys.stderr)
    return 1


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
