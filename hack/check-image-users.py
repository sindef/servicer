#!/usr/bin/env python3
"""Fail when a runAsNonRoot pod in a rendered manifest points at a root image.

The kubelet refuses a container whose image user is root when the pod sets
``runAsNonRoot: true``: the container ends up in CreateContainerConfigError and
the Deployment never reaches Ready replicas. A grep for the ``runAsNonRoot``
marker in the manifest cannot catch that mismatch, so this check pairs every
image in a runAsNonRoot workload with the Containerfile that builds it and
requires a non-root user there.

An image passes when its Containerfile either:

* ends with a ``USER <numeric non-root uid>`` line, or
* builds from a distroless ``:nonroot`` runtime image and sets no later USER.

Images that this repository does not build (``servicer-<component>`` with no
matching ``Containerfile.<component>``) are skipped.

Usage: hack/check-image-users.py <rendered-manifest> [repo-root]
Exit code 0 means every runAsNonRoot image configures a non-root user.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

IMAGE_RE = re.compile(r"^\s*(?:-\s*)?image:\s*(\S+)\s*$")
# Matches ghcr.io/<owner>/servicer-<component>[:<tag>][@sha256:<digest>]; the
# component suffix is what maps the image back to its Containerfile.
COMPONENT_RE = re.compile(
    r"/servicer-(?P<component>[a-z][a-z0-9-]*)(?::[^/@]+)?(?:@sha256:[0-9a-f]+)?$"
)
FROM_RE = re.compile(r"^\s*FROM\s+(?P<base>\S+)", re.IGNORECASE)
USER_RE = re.compile(r"^\s*USER\s+(?P<user>\S+)", re.IGNORECASE)
NONROOT_BASE_RE = re.compile(r":nonroot(?:@|$)")


def manifest_documents(text: str) -> list[list[str]]:
    """Split a multi-document YAML stream on standalone ``---`` separators."""
    documents: list[list[str]] = [[]]
    for line in text.splitlines():
        if line.strip() == "---":
            documents.append([])
            continue
        documents[-1].append(line)
    return documents


def run_as_non_root_images(text: str) -> list[str]:
    images: list[str] = []
    for document in manifest_documents(text):
        if not any(line.strip() == "runAsNonRoot: true" for line in document):
            continue
        for line in document:
            match = IMAGE_RE.match(line)
            if match:
                images.append(match.group(1).strip("\"'"))
    return images


def containerfile_for(image: str, repo_root: Path) -> Path | None:
    """Map a repository-built image name to its Containerfile, if any."""
    match = COMPONENT_RE.search(image)
    if not match:
        return None
    containerfile = repo_root / f"Containerfile.{match.group('component')}"
    return containerfile if containerfile.is_file() else None


def containerfile_problem(containerfile: Path) -> str | None:
    """Return why a Containerfile cannot satisfy runAsNonRoot, else None."""
    lines = containerfile.read_text(encoding="utf-8").splitlines()

    final_from = None
    final_from_index = -1
    for index, line in enumerate(lines):
        match = FROM_RE.match(line)
        if match:
            final_from = match.group("base")
            final_from_index = index
    if final_from is None:
        return f"{containerfile.name} declares no FROM"

    # A USER in the final stage overrides whatever the base image configures, so
    # it decides the runtime user. USER lines in earlier builder stages are
    # irrelevant and are intentionally ignored.
    final_user = None
    for line in lines[final_from_index + 1 :]:
        match = USER_RE.match(line)
        if match:
            final_user = match.group("user")

    if final_user is not None:
        uid = final_user.split(":", 1)[0]
        if not uid.isdigit():
            return (
                f"{containerfile.name} sets USER {final_user}, which the kubelet "
                "cannot verify as non-root; use a numeric uid"
            )
        if int(uid) == 0:
            return f"{containerfile.name} sets USER {final_user}, which is root"
        return None

    if NONROOT_BASE_RE.search(final_from):
        return None
    return f"{containerfile.name} sets no USER, so the image runs as root"


def main(argv: list[str]) -> int:
    if len(argv) < 2 or len(argv) > 3:
        print("usage: check-image-users.py <rendered-manifest> [repo-root]", file=sys.stderr)
        return 2

    manifest = Path(argv[1])
    if not manifest.is_file():
        print(f"{manifest}: rendered manifest not found", file=sys.stderr)
        return 1
    repo_root = Path(argv[2]) if len(argv) == 3 else Path(__file__).resolve().parent.parent

    failures: list[str] = []
    for image in sorted(set(run_as_non_root_images(manifest.read_text(encoding="utf-8")))):
        containerfile = containerfile_for(image, repo_root)
        if containerfile is None:
            continue
        problem = containerfile_problem(containerfile)
        if problem:
            failures.append(f"{image}: {problem}")

    if failures:
        print(
            "production workloads set runAsNonRoot, but these images run as root:",
            file=sys.stderr,
        )
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print(
            "Fix the Containerfile to set an unprivileged numeric USER, or drop "
            "runAsNonRoot from the workload.",
            file=sys.stderr,
        )
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
