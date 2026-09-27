#!/usr/bin/env python3
"""Contract tests for staging PR resolution when workflow_run PR metadata is empty."""

from __future__ import annotations

import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("validate_staging_workflow_pr", ROOT / "scripts/validate-staging-workflow-pr.py")
MODULE = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
SPEC.loader.exec_module(MODULE)

REPOSITORY = "ukyovfx/kitsusync"
SHA = "d3906effdfa809b779fde3a758bd6bd2d7066fbb"


def workflow_run(**overrides):
    value = {
        "event": "pull_request",
        "conclusion": "success",
        "head_sha": SHA,
        "head_repository": {"full_name": REPOSITORY},
        "pull_requests": [],
    }
    value.update(overrides)
    return value


def pr(**overrides):
    value = {
        "number": 221,
        "state": "open",
        "base": {"ref": "master", "repo": {"full_name": REPOSITORY}},
        "head": {"sha": SHA, "repo": {"full_name": REPOSITORY}},
    }
    value.update(overrides)
    return value


class StagingWorkflowPRResolutionTests(unittest.TestCase):
    def test_empty_workflow_run_pull_requests_resolves_from_github_association(self):
        number, sha = MODULE.resolve_trusted_pr(workflow_run(), REPOSITORY, [pr()])
        self.assertEqual((number, sha), (221, SHA))

    def test_paginated_github_api_response_flattens(self):
        self.assertEqual(MODULE.flatten_api_pages([[pr()], []]), [pr()])
        self.assertEqual(MODULE.flatten_api_pages([[]]), [])

    def test_same_repository_open_pr_targeting_master_is_accepted(self):
        self.assertEqual(MODULE.resolve_trusted_pr(workflow_run(), REPOSITORY, [pr()])[0], 221)

    def test_wrong_base_is_rejected(self):
        wrong_base = pr(base={"ref": "develop", "repo": {"full_name": REPOSITORY}})
        with self.assertRaises(MODULE.ResolutionError):
            MODULE.resolve_trusted_pr(workflow_run(), REPOSITORY, [wrong_base])

    def test_fork_or_untrusted_repository_is_rejected(self):
        fork_pr = pr(head={"sha": SHA, "repo": {"full_name": "contributor/kitsusync"}})
        with self.assertRaises(MODULE.ResolutionError):
            MODULE.resolve_trusted_pr(workflow_run(), REPOSITORY, [fork_pr])
        fork_run = workflow_run(head_repository={"full_name": "contributor/kitsusync"})
        with self.assertRaises(MODULE.ResolutionError):
            MODULE.resolve_trusted_pr(fork_run, REPOSITORY, [pr()])

    def test_wrong_sha_is_rejected(self):
        stale_pr = pr(head={"sha": "0" * 40, "repo": {"full_name": REPOSITORY}})
        with self.assertRaises(MODULE.ResolutionError):
            MODULE.resolve_trusted_pr(workflow_run(), REPOSITORY, [stale_pr])

    def test_non_success_or_non_pull_request_run_is_rejected(self):
        for event in (workflow_run(conclusion="failure"), workflow_run(event="push")):
            with self.subTest(event=event["event"], conclusion=event["conclusion"]):
                with self.assertRaises(MODULE.ResolutionError):
                    MODULE.resolve_trusted_pr(event, REPOSITORY, [pr()])

    def test_multiple_matching_prs_fail_closed(self):
        with self.assertRaises(MODULE.ResolutionError):
            MODULE.resolve_trusted_pr(workflow_run(), REPOSITORY, [pr(), pr(number=222)])

    def test_workflow_uses_api_resolution_before_deployment_credentials(self):
        workflow = (ROOT / ".github/workflows/staging-deploy.yml").read_text(encoding="utf-8")
        self.assertNotIn("workflow_run.pull_requests[0]", workflow)
        self.assertIn("commits/$candidate_sha/pulls", workflow)
        self.assertIn("python3 scripts/validate-staging-workflow-pr.py", workflow)
        self.assertIn("pull-requests: read", workflow)
        resolution = workflow.index("Resolve trusted same-repository PR targeting master")
        config = workflow.index("Check one-time staging automation configuration")
        tailscale = workflow.index("Connect ephemeral GitHub-hosted runner to Tailscale")
        self.assertLess(resolution, config)
        self.assertLess(resolution, tailscale)


if __name__ == "__main__":
    unittest.main()
