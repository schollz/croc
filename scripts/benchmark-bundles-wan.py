#!/usr/bin/env python3
"""WAN comparison using the existing SSH runner shape and original manifests.

Control traffic and file data use the private croc relay, with SSH used only
for orchestration. Both builds use the same relay and fixed transport settings.
It captures croc output through pipes and retains timings, checksums, and logs.
"""

from bundle_bench_common import (
    manifest,
    sha256,
    transfer_secret,
    stop_process,
    drain_output,
)
import argparse
import json
import os
from pathlib import Path
import secrets
import selectors
import shlex
import shutil
import subprocess
import time

from dataclasses import dataclass


@dataclass(frozen=True)
class BenchmarkConfig:
    options: argparse.Namespace
    root: Path
    remote: str
    ssh_command: list[str]
    manifests: dict
    settings: dict


def ssh(config, command, **kwargs):
    return subprocess.run(
        config.ssh_command + [command],
        check=True,
        text=True,
        capture_output=True,
        **kwargs,
    )


def run(config, build, dataset, direction, profile, trial=0, timeout=900):
    options, root, remote_root = config.options, config.root, config.remote
    manifests, settings, ssh_command = (
        config.manifests,
        config.settings,
        config.ssh_command,
    )
    run_id = (
        "bundle-"
        + time.strftime("%H%M%S")
        + "-"
        + secrets.token_hex(3)
        + "-"
        + "-".join([build, dataset, direction, profile])
    )
    secret = transfer_secret()
    work = root / "work" / run_id
    work.mkdir(parents=True)
    dest = work / "received"
    if direction == "down":
        dest.mkdir()
    rdest = remote_root + "/receive/" + run_id
    if direction == "up":
        ssh(config, "mkdir -p " + shlex.quote(rdest))
    common = [
        "--relay6",
        "",
        "--debug",
        "--yes",
        "--ignore-stdin",
        "--disable-clipboard",
        "--overwrite",
        "--relay",
        options.relay_host + ":" + str(settings["port"]),
        "--pass",
        settings["password"],
    ]
    if dataset == "large" or profile.endswith("nocompress"):
        common += ["--no-compress"]
    sender_extra = ["send"] + [
        "--no-local",
        "--transport",
        "relay",
        "--transfers",
        "8",
        "--port",
        str(settings["port"]),
        "--hash",
        "xxhash",
    ]
    if "zip" in profile:
        sender_extra += ["--zip"]
    local_source = str(options.video) if dataset == "large" else str(options.source)
    remote_source = (
        options.remote_video if dataset == "large" else options.remote_source
    )
    if direction == "up":
        local_args = common + sender_extra + [local_source]
        remote_args = common + ["--out", rdest]
    else:
        remote_args = common + sender_extra + [remote_source]
        local_args = common + ["--out", str(dest)]
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("CROC_") or key.upper() in (
            "HTTP_PROXY",
            "HTTPS_PROXY",
            "ALL_PROXY",
            "SOCKS5_PROXY",
        ):
            env.pop(key)
    config_directory = root / "config"
    config_directory.mkdir(exist_ok=True)
    env["CROC_CONFIG_DIR"] = str(config_directory)
    env["CROC_SECRET"] = secret
    (config_directory / "version-check.json").write_text(
        json.dumps(
            {
                "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "latest_version": "11.5.5",
            }
        )
    )
    record = {
        "id": run_id,
        "harness_version": 4,
        "trial": trial,
        "build": build,
        "dataset": dataset,
        "direction": direction,
        "profile": profile,
        "bytes": sum(x["size"] for x in manifests[dataset].values()),
        "files": len(manifests[dataset]),
        "local_args": local_args,
        "remote_args": remote_args,
        "destination": rdest if direction == "up" else str(dest),
    }
    # Passwords are redacted in saved metadata; runnable commands use settings.json.
    for args in (record["local_args"], record["remote_args"]):
        args[args.index("--pass") + 1] = "<private-relay-password>"
    local_args = [
        settings["password"] if a == "<private-relay-password>" else a
        for a in local_args
    ]
    remote_args = [
        settings["password"] if a == "<private-relay-password>" else a
        for a in remote_args
    ]
    (root / "active.json").write_text(json.dumps(record, indent=2))
    print("START " + run_id, flush=True)
    started = time.monotonic()
    remote = subprocess.Popen(
        ssh_command
        + ["python3 " + shlex.quote(remote_root + "/bundle_remote_runner.py")],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    remote.stdin.write(
        json.dumps(
            {
                "id": run_id,
                "build": build,
                "secret": secret,
                "args": remote_args,
                "timeout": timeout,
                "binary": (
                    options.remote_base if build == "base" else options.remote_new
                ),
            }
        )
    )
    remote.stdin.close()
    ready = remote.stdout.readline()
    if not ready:
        raise RuntimeError("Remote did not start: " + remote.stderr.read())
    event = json.loads(ready)
    assert event["event"] == "started", event
    with (root / "logs" / (run_id + "-local.log")).open("wb") as logfile:
        local = subprocess.Popen(
            [str(options.local_base if build == "base" else options.local_new)]
            + local_args,
            cwd=work,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        drain = drain_output(local.stdout, logfile)
        selector = selectors.DefaultSelector()
        selector.register(remote.stdout, selectors.EVENT_READ)
        remote_result = None
        next_notice = time.monotonic() + 20
        while True:
            for key, _ in selector.select(timeout=0.2):
                line = key.fileobj.readline()
                if line:
                    candidate = json.loads(line)
                    if candidate["event"] == "finished":
                        remote_result = candidate
                else:
                    selector.unregister(key.fileobj)
            if local.poll() is not None and remote_result is not None:
                break
            elapsed = time.monotonic() - started
            failed = local.poll() not in (None, 0) or (
                remote_result is not None and remote_result.get("returncode") != 0
            )
            if elapsed > timeout + 15 or (failed and elapsed > 5):
                stop_process(local, group=True)
                # Kill only the croc process created for this particular run.
                ssh(config, "kill -TERM " + str(event["pid"]) + " 2>/dev/null || true")
                if remote_result is None:
                    line = remote.stdout.readline()
                    if line:
                        remote_result = json.loads(line)
                break
            if time.monotonic() > next_notice:
                print(
                    "RUNNING " + run_id + " " + str(round(elapsed, 1)) + "s", flush=True
                )
                next_notice = time.monotonic() + 20
        record["seconds"] = time.monotonic() - started
        record["local_returncode"] = local.wait()
        drain.join()
        record["remote_returncode"] = (remote_result or {}).get("returncode")
        record["remote_seconds"] = (remote_result or {}).get("seconds")
        (root / "logs" / (run_id + "-remote.log")).write_text(
            (remote_result or {}).get("log", "")
        )
        remote.wait(timeout=10)
    if record["local_returncode"] == 0 and record["remote_returncode"] == 0:
        if direction == "up":
            code = (
                "import sys;sys.path.insert(0,"
                + repr(remote_root)
                + ");from bundle_remote_runner import manifest;from pathlib import Path;import json;print(json.dumps(manifest(Path("
                + repr(rdest)
                + "))))"
            )
            got = json.loads(ssh(config, "python3 -c " + shlex.quote(code)).stdout)
        else:
            got = manifest(dest)
        record["verified"] = got == manifests[dataset]
        record["received_files"] = len(got)
        if not record["verified"]:
            record["missing"] = sorted(set(manifests[dataset]) - set(got))
            record["extra"] = sorted(set(got) - set(manifests[dataset]))
            record["different"] = [
                key
                for key in got
                if key in manifests[dataset] and got[key] != manifests[dataset][key]
            ]
    else:
        record["verified"] = False
    if direction == "up":
        code = (
            "from pathlib import Path;import shutil;p=Path("
            + repr(rdest)
            + ");assert p.parent==Path("
            + repr(remote_root + "/receive")
            + ') and p.name.startswith("bundle-");shutil.rmtree(p)'
        )
        ssh(config, "python3 -c " + shlex.quote(code))
    elif record["verified"]:
        shutil.rmtree(dest)
    record["MBps"] = (
        record["bytes"] / record["seconds"] / 1e6 if record["verified"] else None
    )
    with (root / "results.jsonl").open("a") as stream:
        stream.write(json.dumps(record) + "\n")
    (root / "active.json").unlink(missing_ok=True)
    print(
        "RESULT "
        + json.dumps(
            {
                k: record.get(k)
                for k in (
                    "id",
                    "seconds",
                    "MBps",
                    "verified",
                    "local_returncode",
                    "remote_returncode",
                    "missing",
                    "extra",
                    "different",
                )
            }
        ),
        flush=True,
    )
    return record


def main():
    parser = argparse.ArgumentParser(
        description="Paired WAN measurements against a real frozen client; inputs must be frozen outside the checkout."
    )
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--host", required=True)
    parser.add_argument("--relay-host", required=True)
    parser.add_argument("--remote-root", required=True)
    parser.add_argument("--relay-settings", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--video", type=Path, required=True)
    parser.add_argument("--remote-source", required=True)
    parser.add_argument("--remote-video", required=True)
    parser.add_argument("--local-base", type=Path, required=True)
    parser.add_argument("--local-new", type=Path, required=True)
    parser.add_argument("--remote-base", required=True)
    parser.add_argument("--remote-new", required=True)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument(
        "--datasets", nargs="+", choices=["src", "large"], default=["src", "large"]
    )
    parser.add_argument(
        "--directions", nargs="+", choices=["up", "down"], default=["up", "down"]
    )
    parser.add_argument(
        "--profiles",
        nargs="+",
        choices=["raw", "zip"],
        help="Restrict repeat measurements to selected existing modes",
    )
    parser.add_argument(
        "--resume",
        action="store_true",
        help="Reuse verified trials after checking binary and input digests",
    )
    options = parser.parse_args()
    root = options.artifacts.resolve()
    root.mkdir(parents=True, exist_ok=True)
    (root / "logs").mkdir(exist_ok=True)
    remote_root = options.remote_root
    ssh_command = [
        "ssh",
        "-o",
        "BatchMode=yes",
        "-o",
        "ControlMaster=auto",
        "-o",
        "ControlPersist=1800",
        "-o",
        "ControlPath=" + str(root / "ssh-control"),
        options.host,
    ]
    # Establish the persistent master with detached stdio. Otherwise its
    # background process can retain capture_output pipes after the command ends.
    subprocess.run(
        ssh_command + ["true"],
        check=True,
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    manifests = json.loads(options.manifest.read_text())
    settings = json.loads(options.relay_settings.read_text())
    config = BenchmarkConfig(
        options, root, remote_root, ssh_command, manifests, settings
    )
    remote_paths = [options.remote_base, options.remote_new]
    code = (
        "from pathlib import Path;import sys,json;sys.path.insert(0,"
        + repr(remote_root)
        + ");from bundle_bench_common import sha256;print(json.dumps({p:sha256(Path(p)) for p in "
        + repr(remote_paths)
        + "}))"
    )
    checks = {
        str(path): sha256(path) for path in [options.local_base, options.local_new]
    }
    checks.update(json.loads(ssh(config, "python3 -c " + shlex.quote(code)).stdout))
    frozen = {"binary_sha256": checks, "input_manifests": manifests}
    record_path = root / "frozen-inputs-and-builds.json"
    if record_path.exists() and json.loads(record_path.read_text()) != frozen:
        raise SystemExit(
            "Benchmark binaries or frozen manifests changed; use a fresh artifact directory"
        )
    record_path.write_text(json.dumps(frozen, indent=2) + "\n")
    results_path = root / "results.jsonl"
    completed = set()
    if results_path.exists():
        if not options.resume:
            raise SystemExit(
                "Results already exist; use --resume or a fresh artifact directory"
            )
        completed = {
            (r["dataset"], r["direction"], r["profile"], r["trial"], r["build"])
            for line in results_path.read_text().splitlines()
            if (r := json.loads(line)).get("verified")
        }
    for dataset in options.datasets:
        for direction in options.directions:
            for profile in (["raw", "zip"] if dataset == "src" else ["raw"]):
                if options.profiles and profile not in options.profiles:
                    continue
                for trial in range(options.repeats):
                    builds = ["base", "new"] if trial % 2 == 0 else ["new", "base"]
                    for build in builds:
                        if (dataset, direction, profile, trial, build) in completed:
                            continue
                        result = run(config, build, dataset, direction, profile, trial)
                        if not result["verified"]:
                            raise SystemExit(
                                "Independent verification failed: " + result["id"]
                            )


if __name__ == "__main__":
    main()
