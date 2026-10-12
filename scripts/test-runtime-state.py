#!/usr/bin/env python3
"""Snapshot reconstruction regressions; optional real stopped-create roundtrip."""

import copy
import io
import importlib.machinery
import importlib.util
import json
import os
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

ROOT = pathlib.Path(__file__).resolve().parents[1]
loader = importlib.machinery.SourceFileLoader("runtime_state", str(ROOT / "deploy/kitsusync-runtime-state"))
spec = importlib.util.spec_from_loader(loader.name, loader)
runtime = importlib.util.module_from_spec(spec)
loader.exec_module(runtime)


def fixture():
    return {
        "Id": "a" * 64, "Name": "/kitsusync-app-1", "Image": "sha256:" + "b" * 64,
        "Config": {"Image": "kitsusync:old", "Hostname": "a" * 12, "User": "10001:10001",
                   "Env": ["APP_ENV=production", "PRIVATE=do-not-print"], "Cmd": ["./kitsu-discord"],
                   "Entrypoint": None, "WorkingDir": "/app", "Labels": {"significant": "yes"}},
        "HostConfig": {"AutoRemove": False, "NetworkMode": "ks-net", "PidMode": "", "IpcMode": "private",
                       "Binds": ["/runtime/conf.toml:/app/conf.toml:ro", "/runtime/data:/app/data:rw"],
                       "PortBindings": {"8090/tcp": [{"HostIp": "127.0.0.1", "HostPort": "8090"}]},
                       "RestartPolicy": {"Name": "unless-stopped", "MaximumRetryCount": 0},
                       "SecurityOpt": ["no-new-privileges:true"], "Memory": 1073741824},
        "Mounts": [{"Type": "bind", "Source": "/runtime/conf.toml", "Destination": "/app/conf.toml",
                    "Mode": "ro", "RW": False, "Propagation": "rprivate"},
                   {"Type": "bind", "Source": "/runtime/data", "Destination": "/app/data",
                    "Mode": "rw", "RW": True, "Propagation": "rprivate"}],
        "NetworkSettings": {"Networks": {"ks-net": {"NetworkID": "c" * 64,
                            "EndpointID": "d" * 64, "IPAddress": "172.20.0.2", "Gateway": "172.20.0.1",
                            "IPPrefixLen": 16, "Aliases": ["kitsusync-app-1", "a" * 12, "app"],
                            "IPAMConfig": {"IPv4Address": "172.20.0.2"}, "DriverOpts": None,
                            "MacAddress": "02:42:ac:14:00:02"}}},
    }


class RuntimeStateTests(unittest.TestCase):
    def test_bounded_stdin_inspect_input(self):
        raw = json.dumps(fixture()).encode("utf-8")
        stdin = type("Input", (), {"buffer": io.BytesIO(raw)})()
        with mock.patch.object(runtime.sys, "stdin", stdin):
            self.assertEqual(runtime.read_json("-"), fixture())

    def test_full_config_round_trip_plan(self):
        original = fixture()
        plan = runtime.make_plan(original)
        self.assertEqual(runtime.check_plan(plan), plan)
        self.assertEqual(plan["request"]["HostConfig"],
                         runtime.normalized_host_config(original["HostConfig"]))
        self.assertEqual(plan["request"]["Image"], original["Image"])
        self.assertEqual(plan["request"]["Env"], original["Config"]["Env"])
        endpoint = plan["request"]["NetworkingConfig"]["EndpointsConfig"]["ks-net"]
        self.assertEqual(endpoint["NetworkID"], "c" * 64)
        self.assertEqual(endpoint["Aliases"], ["app"])
        self.assertNotIn("EndpointID", endpoint)
        self.assertNotIn("IPAddress", endpoint)

    def test_expected_instance_changes_only(self):
        original = fixture()
        restored = copy.deepcopy(original)
        restored["Id"] = "e" * 64
        restored["Config"]["Hostname"] = "e" * 12
        restored["Config"]["Image"] = original["Image"]
        restored["Config"]["NetworkDisabled"] = False
        restored["Config"]["MacAddress"] = ""
        restored["Mounts"].reverse()
        endpoint = restored["NetworkSettings"]["Networks"]["ks-net"]
        endpoint["EndpointID"] = "f" * 64
        endpoint["Aliases"] = ["e" * 12, "app", "kitsusync-app-1"]
        runtime.compare(runtime.make_plan(original), restored)

    def test_probe_name_requires_explicit_comparison(self):
        source = fixture()
        probe = fixture()
        probe["Name"] = "/ks-probe"
        probe["NetworkSettings"]["Networks"]["ks-net"]["Aliases"] = ["ks-probe", "app"]
        plan = runtime.make_plan(source)
        with self.assertRaises(runtime.StateError):
            runtime.compare(plan, probe)
        runtime.compare(plan, probe, "ks-probe")

    def test_each_significant_mismatch_rejected(self):
        changes = [
            lambda x: x.update(Image="sha256:" + "f" * 64),
            lambda x: x["Config"].update(User="0:0"),
            lambda x: x["Config"].update(Env=["APP_ENV=development"]),
            lambda x: x["Config"].update(Cmd=["false"]),
            lambda x: x["Config"].update(Entrypoint=["/bin/sh"]),
            lambda x: x["Config"]["Labels"].update(significant="no"),
            lambda x: x["HostConfig"].update(Memory=0),
            lambda x: x["HostConfig"]["RestartPolicy"].update(Name="no"),
            lambda x: x["HostConfig"]["PortBindings"]["8090/tcp"][0].update(HostIp="0.0.0.0"),
            lambda x: x["Mounts"].pop(0),
            lambda x: x["Mounts"][0].update(RW=True),
            lambda x: x["NetworkSettings"]["Networks"]["ks-net"].update(NetworkID="f" * 64),
            lambda x: x["NetworkSettings"]["Networks"]["ks-net"].update(DriverOpts={"custom": "changed"}),
            lambda x: x["NetworkSettings"]["Networks"]["ks-net"]["IPAMConfig"].update(IPv4Address="172.20.0.3"),
        ]
        expected = runtime.make_plan(fixture())
        for index, change in enumerate(changes):
            with self.subTest(index=index):
                changed = fixture()
                change(changed)
                with self.assertRaises(runtime.StateError):
                    runtime.compare(expected, changed)

    def test_unsupported_dependencies_fail_before_create(self):
        for key, value in (("AutoRemove", True), ("NetworkMode", "container:other"),
                           ("PidMode", "container:other"), ("VolumesFrom", ["other"]),
                           ("PublishAllPorts", True)):
            with self.subTest(key=key):
                source = fixture()
                source["HostConfig"][key] = value
                with self.assertRaises(runtime.StateError):
                    runtime.make_plan(source)

    def test_dynamic_port_rejected(self):
        source = fixture()
        source["HostConfig"]["PortBindings"]["8090/tcp"][0]["HostPort"] = "0"
        with self.assertRaises(runtime.StateError):
            runtime.make_plan(source)

    def test_implicit_volume_rejected(self):
        source = fixture()
        source["Mounts"].append({"Type": "volume", "Name": "random", "Destination": "/implicit"})
        with self.assertRaises(runtime.StateError):
            runtime.make_plan(source)

    def test_volume_copy_up_is_rejected_before_probe(self):
        source = fixture()
        source["HostConfig"]["Mounts"] = [{"Type": "volume", "Source": "stored-data", "Target": "/extra"}]
        source["Mounts"].append({"Type": "volume", "Name": "stored-data", "Destination": "/extra",
                                 "Source": "/var/lib/docker/volumes/stored-data/_data", "Driver": "local"})
        with self.assertRaises(runtime.StateError):
            runtime.make_plan(source)
        source["HostConfig"]["Mounts"][0]["VolumeOptions"] = {"NoCopy": True}
        runtime.check_plan(runtime.make_plan(source))
        source["HostConfig"]["Mounts"][0]["Source"] = ""
        with self.assertRaises(runtime.StateError):
            runtime.make_plan(source)

    def test_legacy_named_volume_requires_nocopy(self):
        source = fixture()
        source["HostConfig"]["Binds"].append("stored-data:/extra:ro")
        source["Mounts"].append({"Type": "volume", "Name": "stored-data", "Destination": "/extra",
                                 "Source": "/var/lib/docker/volumes/stored-data/_data", "Driver": "local"})
        with self.assertRaises(runtime.StateError):
            runtime.make_plan(source)
        source["HostConfig"]["Binds"][-1] += ",nocopy"
        runtime.check_plan(runtime.make_plan(source))

    def test_symlink_bind_source_is_rejected_before_creation(self):
        engine = mock.Mock()
        source = fixture()
        engine.request.side_effect = [{"Id": source["Image"]}, {"Id": "c" * 64, "Name": "ks-net"}]
        with mock.patch.object(runtime.os.path, "exists", return_value=True), \
             mock.patch.object(runtime.os.path, "realpath", return_value="/redirected/conf.toml"):
            with self.assertRaises(runtime.StateError):
                runtime.create(runtime.make_plan(source), engine)
        self.assertEqual([call.args[0] for call in engine.request.call_args_list], ["GET", "GET"])

    def test_unknown_network_input_not_silently_dropped(self):
        source = fixture()
        source["NetworkSettings"]["Networks"]["ks-net"]["FutureSignificantSetting"] = True
        with self.assertRaises(runtime.StateError):
            runtime.make_plan(source)

    def test_plan_corruption_rejected(self):
        plan = runtime.make_plan(fixture())
        plan["request"]["HostConfig"]["Binds"] = []
        with self.assertRaises(runtime.StateError):
            runtime.check_plan(plan)

    def test_create_validates_dependencies_before_mutating(self):
        engine = mock.Mock()
        engine.request.return_value = {"Id": "sha256:" + "f" * 64}
        with self.assertRaises(runtime.StateError):
            runtime.create(runtime.make_plan(fixture()), engine)
        self.assertEqual([call.args[0] for call in engine.request.call_args_list], ["GET"])

    def test_creation_warning_removes_only_exact_stopped_container(self):
        engine = mock.Mock()
        created_id = "e" * 64
        engine.request.side_effect = [{"Id": created_id, "Warnings": ["private daemon details"]}, {}]
        with mock.patch.object(runtime, "validate_dependencies"):
            with self.assertRaises(runtime.StateError) as error:
                runtime.create(runtime.make_plan(fixture()), engine)
        self.assertNotIn("private", str(error.exception))
        self.assertEqual(engine.request.call_args_list[-1].args,
                         ("DELETE", "/containers/" + created_id + "?force=false&v=false"))

    def test_failed_warning_cleanup_retains_safe_identity(self):
        engine = mock.Mock()
        created_id = "e" * 64
        engine.request.side_effect = [{"Id": created_id, "Warnings": ["private daemon details"]},
                                      runtime.StateError("private daemon failure")]
        with mock.patch.object(runtime, "validate_dependencies"):
            with self.assertRaises(runtime.CleanupError) as error:
                runtime.create(runtime.make_plan(fixture()), engine)
        self.assertEqual(error.exception.container_id, created_id)
        self.assertNotIn("private", str(error.exception))

    def test_invalid_created_identity_is_never_deleted(self):
        engine = mock.Mock()
        engine.request.return_value = {"Id": "untrusted-name", "Warnings": ["warning"]}
        with mock.patch.object(runtime, "validate_dependencies"):
            with self.assertRaises(runtime.StateError):
                runtime.create(runtime.make_plan(fixture()), engine)
        self.assertEqual([call.args[0] for call in engine.request.call_args_list], ["POST"])

    def test_difference_diagnostic_withholds_values(self):
        expected = {"host_config": {"SecretSetting": "first-private-value"}}
        actual = {"host_config": {"SecretSetting": "second-private-value"}}
        message = runtime.difference_path(expected, actual)
        self.assertEqual(message, "state.host_config.SecretSetting")
        self.assertNotIn("private-value", message)

    def test_duplicate_config_mac_is_normalized_but_endpoint_mac_is_preserved(self):
        source = fixture()
        endpoint_mac = source["NetworkSettings"]["Networks"]["ks-net"]["MacAddress"]
        source["Config"]["MacAddress"] = "02:42:ac:11:00:02"
        state = runtime.runtime_state(source)
        self.assertEqual(state["config"]["MacAddress"], "")
        self.assertEqual(state["networks"]["ks-net"]["MacAddress"], endpoint_mac)
        self.assertEqual(runtime.make_plan(source)["request"]["MacAddress"], "")

    def test_generated_looking_endpoint_mac_remains_strict_on_address_change(self):
        source = fixture()
        endpoint = source["NetworkSettings"]["Networks"]["ks-net"]
        endpoint["IPAMConfig"] = {}
        plan = runtime.make_plan(source)
        self.assertEqual(plan["state"]["networks"]["ks-net"]["MacAddress"], "02:42:ac:14:00:02")
        restored = copy.deepcopy(source)
        restored["NetworkSettings"]["Networks"]["ks-net"].update(
            IPAddress="172.20.0.3", MacAddress="02:42:ac:14:00:03")
        with self.assertRaisesRegex(runtime.StateError, "state.networks.ks-net.MacAddress"):
            runtime.compare(plan, restored)

    def test_custom_mac_without_static_ip_remains_strict(self):
        source = fixture()
        endpoint = source["NetworkSettings"]["Networks"]["ks-net"]
        endpoint.update(IPAMConfig={}, MacAddress="02:11:22:33:44:55")
        plan = runtime.make_plan(source)
        self.assertEqual(plan["state"]["networks"]["ks-net"]["MacAddress"], "02:11:22:33:44:55")
        restored = copy.deepcopy(source)
        restored["NetworkSettings"]["Networks"]["ks-net"]["MacAddress"] = "02:11:22:33:44:56"
        with self.assertRaisesRegex(runtime.StateError, "state.networks.ks-net.MacAddress"):
            runtime.compare(plan, restored)

    def test_explicit_ipam_and_endpoint_mac_are_both_strict(self):
        source = fixture()
        endpoint = source["NetworkSettings"]["Networks"]["ks-net"]
        endpoint["IPAMConfig"] = {"IPv4Address": "172.20.0.2"}
        plan = runtime.make_plan(source)
        self.assertEqual(plan["state"]["networks"]["ks-net"]["MacAddress"], "02:42:ac:14:00:02")
        restored = copy.deepcopy(source)
        restored["NetworkSettings"]["Networks"]["ks-net"].update(
            IPAddress="172.20.0.3", IPAMConfig={"IPv4Address": "172.20.0.3"},
            MacAddress="02:42:ac:14:00:03")
        with self.assertRaisesRegex(runtime.StateError, "state.networks.ks-net.IPAMConfig|state.networks.ks-net.MacAddress"):
            runtime.compare(plan, restored)

    def test_unset_oom_kill_policy_matches_docker_false_default(self):
        source = fixture()
        source["HostConfig"]["OomKillDisable"] = None
        expected = runtime.runtime_state(source)
        source["HostConfig"]["OomKillDisable"] = False
        self.assertEqual(runtime.runtime_state(source), expected)

    def test_create_attaches_secondary_networks_with_saved_endpoint_identity(self):
        source = fixture()
        source["NetworkSettings"]["Networks"]["ks-extra"] = {
            "NetworkID": "e" * 64, "EndpointID": "f" * 64, "IPAddress": "172.21.0.2",
            "Gateway": "172.21.0.1", "IPPrefixLen": 16, "Aliases": ["extra"],
            "IPAMConfig": {}, "DriverOpts": {}, "MacAddress": "02:42:ac:15:00:02",
            "Links": None, "GwPriority": 0,
        }
        plan = runtime.make_plan(source)

        class FakeEngine:
            def __init__(self):
                self.calls = []

            def request(self, method, path, body=None):
                self.calls.append((method, path, copy.deepcopy(body)))
                if method == "GET" and path.startswith("/images/"):
                    return {"Id": plan["state"]["image_id"]}
                if method == "GET" and path.startswith("/networks/"):
                    network_id = path.rsplit("/", 1)[-1]
                    name = next(k for k, v in plan["state"]["networks"].items()
                                if v["NetworkID"] == network_id)
                    return {"Id": network_id, "Name": name}
                if method == "POST" and path.startswith("/containers/create?"):
                    return {"Id": "1" * 64}
                return {}

        engine = FakeEngine()
        with mock.patch.object(runtime.os.path, "exists", return_value=True), \
                mock.patch.object(runtime.os.path, "realpath", side_effect=lambda path: path):
            runtime.create(plan, engine, "restored")

        creates = [call for call in engine.calls if call[1].startswith("/containers/create?")]
        self.assertEqual(len(creates), 1)
        self.assertEqual(set(creates[0][2]["NetworkingConfig"]["EndpointsConfig"]), {"ks-net"})
        attach = [call for call in engine.calls if call[1].endswith("/connect")]
        self.assertEqual(attach, [("POST", "/networks/" + "e" * 64 + "/connect", {
            "Container": "1" * 64,
            "EndpointConfig": {
                key: value for key, value in plan["request"]["NetworkingConfig"]["EndpointsConfig"]["ks-extra"].items()
                if key != "NetworkID"
            },
        })])

    def test_create_removes_exact_stopped_container_when_secondary_connect_fails(self):
        source = fixture()
        source["NetworkSettings"]["Networks"]["ks-extra"] = {
            "NetworkID": "e" * 64, "EndpointID": "f" * 64, "IPAddress": "172.21.0.2",
            "Gateway": "172.21.0.1", "IPPrefixLen": 16, "Aliases": ["extra"],
            "IPAMConfig": {}, "DriverOpts": {}, "MacAddress": "02:42:ac:15:00:02",
            "Links": None, "GwPriority": 0,
        }
        plan = runtime.make_plan(source)

        class FakeEngine:
            def __init__(self):
                self.calls = []

            def request(self, method, path, body=None):
                self.calls.append((method, path))
                if method == "GET" and path.startswith("/images/"):
                    return {"Id": plan["state"]["image_id"]}
                if method == "GET" and path.startswith("/networks/"):
                    network_id = path.rsplit("/", 1)[-1]
                    name = next(k for k, v in plan["state"]["networks"].items()
                                if v["NetworkID"] == network_id)
                    return {"Id": network_id, "Name": name}
                if method == "POST" and path.startswith("/containers/create?"):
                    return {"Id": "1" * 64}
                if method == "POST" and path.endswith("/connect"):
                    raise runtime.StateError("connect failed")
                return {}

        engine = FakeEngine()
        with mock.patch.object(runtime.os.path, "exists", return_value=True), \
                mock.patch.object(runtime.os.path, "realpath", side_effect=lambda path: path):
            with self.assertRaisesRegex(runtime.StateError, "connect failed"):
                runtime.create(plan, engine, "restored")
        self.assertIn(("DELETE", "/containers/" + "1" * 64 + "?force=false&v=false"), engine.calls)


@unittest.skipUnless(os.environ.get("KITSUSYNC_RUNTIME_DOCKER_TEST") == "1", "Docker integration not requested")
class DockerRoundTripTests(unittest.TestCase):
    def test_real_create_retains_prior_runtime_shape(self):
        image = "kitsusync:" + os.environ["KITSUSYNC_IMAGE_TAG"]
        suffix = str(os.getpid())
        original, restored = "ks-roundtrip-" + suffix, "ks-probe-" + suffix
        network, extra_network = "ks-network-" + suffix, "ks-extra-" + suffix
        def docker(*args):
            return subprocess.check_output(["docker", *args], text=True).strip()
        with tempfile.TemporaryDirectory() as directory:
            config = pathlib.Path(directory) / "conf.toml"
            config.write_text("# nonsecret test fixture\n")
            try:
                docker("network", "create", network)
                docker("network", "create", extra_network)
                docker("run", "-d", "--name", original, "--network", network, "--network-alias", "app",
                       "--user", "10001:10001", "--restart", "unless-stopped", "--memory", "128m",
                       "--security-opt", "no-new-privileges:true", "--env", "APP_ENV=production",
                       "--mount", f"type=bind,src={config},dst=/app/conf.toml,readonly",
                       "--mount", f"type=bind,src={directory},dst=/app/data",
                       "--entrypoint", "/bin/sh", image, "-c", "sleep 300")
                docker("network", "connect", "--alias", "secondary", extra_network, original)
                source = json.loads(docker("inspect", original))[0]
                plan = runtime.make_plan(source)
                self.assertEqual(set(plan["state"]["networks"]), {network, extra_network})
                engine = runtime.Engine()
                identity = runtime.create(plan, engine, restored)
                created = json.loads(docker("inspect", identity))[0]
                self.assertEqual(created["State"]["Status"], "created")
                runtime.compare(plan, created, restored)
                docker("stop", original)
                docker("start", restored)
                runtime.compare(plan, json.loads(docker("inspect", restored))[0], restored)
            finally:
                subprocess.run(["docker", "rm", "-f", original, restored], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                subprocess.run(["docker", "network", "rm", network, extra_network], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    unittest.main()
