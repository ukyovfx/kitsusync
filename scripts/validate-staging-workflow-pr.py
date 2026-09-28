#!/usr/bin/env python3
"""Fail-closed validation for the trusted PR associated with a Staging run SHA."""

from __future__ import annotations

import json
import os
import re
import sys
from pathlib import Path
from typing import Any, Iterable


class ResolutionError(ValueError):
    pass


def flatten_api_pages(payload: Any) -> list[dict[str, Any]]:
    if not isinstance(payload, list):
        raise ResolutionError("invalid_api_response")
    items: list[dict[str, Any]] = []
    for page in payload:
        if not isinstance(page, list) or any(not isinstance(item, dict) for item in page):
            raise ResolutionError("invalid_api_response")
        items.extend(page)
    return items


def resolve_trusted_pr(workflow_run: dict[str, Any], repository: str, prs: Iterable[dict[str, Any]]) -> tuple[int, str]:
    if workflow_run.get("event") != "pull_request" or workflow_run.get("conclusion") != "success":
        raise ResolutionError("workflow_run_not_successful_pull_request")
    head_repository = workflow_run.get("head_repository")
    if not isinstance(head_repository, dict) or head_repository.get("full_name") != repository:
        raise ResolutionError("workflow_run_repository_untrusted")
    candidate_sha = workflow_run.get("head_sha")
    if not isinstance(candidate_sha, str) or re.fullmatch(r"[0-9a-f]{40}", candidate_sha) is None:
        raise ResolutionError("invalid_sha")

    matches = []
    for pr in prs:
        base = pr.get("base") if isinstance(pr, dict) else None
        head = pr.get("head") if isinstance(pr, dict) else None
        base_repo = base.get("repo") if isinstance(base, dict) else None
        head_repo = head.get("repo") if isinstance(head, dict) else None
        if (
            pr.get("state") == "open"
            and isinstance(base, dict)
            and base.get("ref") == "master"
            and isinstance(base_repo, dict)
            and base_repo.get("full_name") == repository
            and isinstance(head, dict)
            and head.get("sha") == candidate_sha
            and isinstance(head_repo, dict)
            and head_repo.get("full_name") == repository
        ):
            matches.append(pr)
    if len(matches) != 1:
        raise ResolutionError("no_unique_same_repository_master_pr")
    number = matches[0].get("number")
    if not isinstance(number, int) or number <= 0:
        raise ResolutionError("invalid_pr_number")
    return number, candidate_sha


def main() -> int:
    repository = os.environ.get("GITHUB_REPOSITORY", "")
    candidate_sha = os.environ.get("CANDIDATE_SHA", "")
    try:
        workflow_run = json.loads(os.environ.get("WORKFLOW_RUN_JSON", ""))
        if not isinstance(workflow_run, dict):
            raise ResolutionError("invalid_workflow_run")
        api_pages = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
        pr_number, resolved_sha = resolve_trusted_pr(workflow_run, repository, flatten_api_pages(api_pages))
        if candidate_sha != resolved_sha:
            raise ResolutionError("candidate_sha_mismatch")
    except (OSError, json.JSONDecodeError, IndexError, ResolutionError) as error:
        reason = str(error) if isinstance(error, ResolutionError) else "invalid_input"
        print(f"STAGING_PR_RESOLUTION=FAIL reason={reason}")
        return 1
    print(f"STAGING_PR_RESOLUTION=PASS number={pr_number} sha={resolved_sha} base=master source=same-repository")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
