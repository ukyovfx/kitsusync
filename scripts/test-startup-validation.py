#!/usr/bin/env python3
"""Execute the deploy wrapper's real startup validators against deterministic I/O."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parents[1]
MOCK = r'''#!/usr/bin/python3
import json, os, pathlib, sys, time
root = pathlib.Path(os.environ["STARTUP_FIXTURE"])
settings = json.loads((root / "settings").read_text())
args = sys.argv[1:]
counter = root / "attempt"
attempt = int(counter.read_text()) if counter.exists() else 0
case = settings["case"]
if pathlib.Path(sys.argv[0]).name == "docker":
    if args[0] == "port":
        print("0.0.0.0:8090" if case == "wrong-port" else "127.0.0.1:8090")
    elif args[2] == "{{.State.Status}}":
        attempt += 1
        counter.write_text(str(attempt))
        if case == "hung-docker": time.sleep(20)
        print("exited" if case == "exited" else "running")
    elif args[2] == "{{.Image}}":
        print("sha256:wrong" if case == "wrong-legacy-image" else "sha256:previous")
    elif ".State.Health" in args[2]:
        print(case if case in ("none", "unhealthy") else "starting" if case == "starting" or (case in ("transition", "legacy-transition") and attempt == 1) else "healthy")
    elif "image.revision" in args[2]:
        print("wrong" if case == "wrong-revision" else "revision")
    elif "image.source-id" in args[2]:
        print("wrong" if case == "wrong-source" else "source")
    elif "image.version" in args[2]:
        print("wrong" if case == "wrong-version" else "0.4.6")
    else: sys.exit(90)
else:
    url = args[-1]
    with (root / "requests").open("a") as out: out.write(url + "\n")
    if case == "hung-http": time.sleep(20)
    if case == "transition" and attempt in (2, 3):
        print("fixture-private-response-must-not-escape", file=sys.stderr)
        sys.exit(7 if attempt == 2 else 56)
    if url == "http://127.0.0.1:8090/health":
        if case == "legacy-transition" and attempt == 2: sys.exit(7)
        if settings["legacy"]: print("503" if case == "bad-health" else "200")
        elif case == "bad-health": sys.exit(22)
        else: print('{"status":"ok"}')
    elif url.endswith("/ready"):
        if settings["legacy"]:
            print("200" if case == "wrong-legacy-ready" else "404")
        else:
            state = "setup_required" if case == "setup" or (case == "transition" and attempt == 4) else "ready"
            if case == "degraded": state = "degraded"
            revision = "wrong" if case == "wrong-readiness-identity" else "revision"
            print(json.dumps({"status":state,"build":{"base_commit":revision,"build_source_id":"source"},"private":"fixture-private-response-must-not-escape"}, separators=(",", ":")))
            print("503" if case == "wrong-ready-code" else "200" if state == "ready" else "503")
    else:
        print("500" if case == "bad-admin" else settings.get("admin_code", "302"))
'''


@unittest.skipUnless(os.name == "posix", "requires Linux/POSIX tools")
class StartupValidation(unittest.TestCase):
    def run_case(self, case, *, mode="normal", legacy=False, timeout=2, passes=False, stage=None, admin_code="302"):
        self.assertNotEqual(os.geteuid(), 0, "run wrapper tests as an unprivileged user")
        with tempfile.TemporaryDirectory(prefix="kitsusync-startup-") as temp:
            root = Path(temp)
            (root / "settings").write_text(json.dumps(dict(case=case, legacy=legacy, admin_code=admin_code)))
            for tool in ("docker", "curl"):
                path = root / tool
                path.write_text(MOCK.replace("#!/usr/bin/python3", "#!" + sys.executable, 1))
                path.chmod(0o700)
            env = {k: v for k, v in os.environ.items() if not k.startswith(("DOCKER_", "COMPOSE_"))}
            env.update(STARTUP_FIXTURE=temp, KITSUSYNC_DEPLOY_TEST_MODE="legacy-runtime" if legacy else "runtime",
                       KITSUSYNC_DEPLOY_TEST_DOCKER_BIN=str(root / "docker"),
                       KITSUSYNC_DEPLOY_TEST_CURL_BIN=str(root / "curl"),
                       KITSUSYNC_DEPLOY_TEST_TIMEOUT=str(timeout))
            args = ["fixture-container", "sha256:previous"] if legacy else ["fixture-container", mode, "revision", "source", "0.4.6"]
            started = time.monotonic()
            result = subprocess.run(["bash", str(ROOT / "deploy/kitsusync-deploy"), *args], env=env,
                                    text=True, capture_output=True, timeout=timeout + 4)
            self.assertEqual(result.returncode == 0, passes, result.stderr)
            self.assertLess(time.monotonic() - started, timeout + 3, "overall deadline exceeded")
            self.assertEqual(result.stdout, "", "validator must not print response or inspect contents")
            self.assertNotIn("fixture-private-response-must-not-escape", result.stderr)
            if stage:
                self.assertIn("runtime validation failed: check=" + stage, result.stderr)
            if passes and not legacy:
                requests = (root / "requests").read_text()
                for path in ("/health", "/ready", "/api/setup/status", "/bot/setup", "/bot/admin", "/bot/admin/health"):
                    self.assertIn("http://127.0.0.1:8090" + path + "\n", requests)
            return int((root / "attempt").read_text())

    def test_transient_starting_refused_reset_setup_then_ready(self):
        self.assertGreaterEqual(self.run_case("transition", timeout=10, passes=True), 5)

    def test_ready_and_expected_auth_boundaries(self):
        for code in ("200", "302", "303", "401", "403"):
            with self.subTest(code=code): self.run_case("ready", passes=True, admin_code=code)

    def test_setup_modes(self):
        self.run_case("setup", stage="readiness")
        self.run_case("setup", mode="legacy-migration", stage="readiness")
        self.run_case("setup", mode="recovery", passes=True)

    def test_persistent_failures_cannot_pass(self):
        for case, stage in (("starting", "docker_health"), ("none", "docker_health"),
                            ("unhealthy", "docker_health"), ("degraded", "readiness"),
                            ("wrong-ready-code", "readiness"),
                            ("bad-health", "local_health"), ("bad-admin", "admin_boundary")):
            with self.subTest(case=case): self.run_case(case, stage=stage)

    def test_identity_and_binding_fail_immediately(self):
        for case, stage in (("wrong-port", "port_binding"), ("exited", "container_state"),
                            ("wrong-revision", "image_revision"), ("wrong-source", "image_source"),
                            ("wrong-version", "image_version"), ("wrong-readiness-identity", "readiness_identity")):
            with self.subTest(case=case): self.assertEqual(self.run_case(case, stage=stage), 1)

    def test_blocked_io_obeys_overall_deadline(self):
        self.run_case("hung-docker", stage="container_state")
        self.run_case("hung-http", stage="local_health")

    def test_legacy_rollback_waits_but_requires_old_identity_and_404(self):
        self.run_case("legacy-transition", legacy=True, timeout=6, passes=True)
        self.assertEqual(self.run_case("wrong-legacy-image", legacy=True, stage="legacy_image"), 1)
        self.run_case("wrong-legacy-ready", legacy=True, stage="legacy_readiness")


if __name__ == "__main__":
    unittest.main()
