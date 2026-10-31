#!/usr/bin/env python3
"""Real pre-extension CLI compatibility; artifacts stay outside the checkout.

python3 scripts/test-bundle-compatibility.py --artifacts /tmp/croc-bundle-compat
The old executable is built from a Git archive of ebcdc31b, never a feature-
disabled new executable. Logs and SHA-256 manifests are retained for review.
"""

from bundle_bench_common import manifest, transfer_secret, stop_process
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import tarfile
import time

BASE = "ebcdc31b413991e4892260b9f43697eb1797f7ca"
REPO = Path(__file__).resolve().parents[1]


def run(command, **kwargs):
    subprocess.run([str(part) for part in command], check=True, **kwargs)


def freeze(artifacts):
    source = artifacts / "frozen-ebcdc31b"
    if not source.exists():
        source.mkdir()
        archive = artifacts / "frozen-source.tar"
        run(["git", "archive", BASE, "--output", archive], cwd=REPO)
        with tarfile.open(archive) as stream:
            stream.extractall(source, filter="data")
        archive.unlink()
    (artifacts / "base-revision.txt").write_text(BASE + "\n")
    return source


def ports(count):
    reserved = []
    try:
        for _ in range(count):
            connection = socket.socket()
            connection.bind(("127.0.0.1", 0))
            reserved.append(connection)
        return [connection.getsockname()[1] for connection in reserved]
    finally:
        for connection in reserved:
            connection.close()


def transfer(sender, receiver, relay, source, output, extra_send=(), extra_receive=()):
    output.mkdir()
    destination = output / "received"
    destination.mkdir()
    secret = transfer_secret()
    common = [
        "--debug",
        "--relay",
        relay,
        "--relay6",
        "",
        "--yes",
        "--ignore-stdin",
        "--disable-clipboard",
    ]
    send_args = [
        sender,
        *common,
        "send",
        "--no-local",
        "--transport",
        "relay",
        *extra_send,
        source,
    ]
    receive_args = [receiver, *common, "--out", destination, *extra_receive]
    started = time.monotonic()
    processes = []
    try:
        with (output / "sender.log").open("w") as send_log, (
            output / "receiver.log"
        ).open("w") as receive_log:
            for command, log, name in [
                (send_args, send_log, "sender"),
                (receive_args, receive_log, "receiver"),
            ]:
                env = dict(
                    os.environ,
                    CROC_SECRET=secret,
                    CROC_CONFIG_DIR=str(output / f"config-{name}"),
                )
                process = subprocess.Popen(
                    [str(part) for part in command],
                    env=env,
                    stdin=subprocess.DEVNULL,
                    stdout=log,
                    stderr=subprocess.STDOUT,
                )
                processes.append(process)
                if name == "sender":
                    time.sleep(0.15)
            for process in reversed(processes):
                if process.wait(timeout=120) != 0:
                    raise RuntimeError(f"Transfer failed; see {output}")
        elapsed = time.monotonic() - started
        expected = manifest(source)
        actual = manifest(destination / source.name)
        if expected != actual:
            raise RuntimeError(f"Path, size, or SHA-256 mismatch; see {output}")
        sender_log = (output / "sender.log").read_text(
            encoding="utf-8", errors="replace"
        )
        receiver_log = (output / "receiver.log").read_text(
            encoding="utf-8", errors="replace"
        )
        result = {
            "seconds": elapsed,
            "files": len(actual),
            "manifest": actual,
            "bundle_requests": sender_log.count("read bundle-request message"),
            "bundle_messages": "bundle-request" in sender_log
            or "bundle-request" in receiver_log,
            "sender": str(sender),
            "receiver": str(receiver),
        }
        (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        return result
    finally:
        for process in processes:
            stop_process(process)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--old", type=Path)
    parser.add_argument("--new", type=Path)
    parser.add_argument("--freeze-only", action="store_true")
    args = parser.parse_args()
    artifacts = args.artifacts.resolve()
    artifacts.mkdir(parents=True, exist_ok=True)
    frozen = freeze(artifacts)
    if args.freeze_only:
        print(frozen)
        return
    suffix = ".exe" if os.name == "nt" else ""
    binaries = artifacts / "bin"
    binaries.mkdir(exist_ok=True)
    old, new = (
        args.old or binaries / f"old{suffix}",
        args.new or binaries / f"new{suffix}",
    )
    for binary, directory, supplied in [(old, frozen, args.old), (new, REPO, args.new)]:
        if not supplied:
            run(["go", "build", "-buildvcs=false", "-o", binary, "."], cwd=directory)
    relay_ports = ports(5)
    relay_address = f"127.0.0.1:{relay_ports[0]}"
    with (artifacts / "relay.log").open("w") as relay_log:
        relay = subprocess.Popen(
            [
                str(new),
                "relay",
                "--host",
                "127.0.0.1",
                "--ports",
                ",".join(map(str, relay_ports)),
            ],
            stdout=relay_log,
            stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 10
            while True:
                try:
                    with socket.create_connection(
                        ("127.0.0.1", relay_ports[0]), timeout=0.2
                    ):
                        break
                except OSError:
                    if time.monotonic() >= deadline:
                        raise RuntimeError("Relay did not start")
                    time.sleep(0.05)
            results = []
            for count in [3, 4, 256]:
                source = artifacts / f"fixture-{count}"
                source.mkdir(exist_ok=True)
                for index in range(count):
                    (source / f"{index:04}.bin").write_bytes(
                        bytes((i * 31 + index) % 251 for i in range(32768 + index))
                    )
                (artifacts / f"fixture-{count}-sha256.json").write_text(
                    json.dumps(manifest(source), indent=2)
                )
                for label, sender, receiver in [
                    ("new-old", new, old),
                    ("old-new", old, new),
                    ("new-new", new, new),
                ]:
                    result = transfer(
                        sender,
                        receiver,
                        relay_address,
                        source,
                        artifacts / f"{label}-{count}",
                    )
                    expected_bundles = (
                        (count + 127) // 128 if label == "new-new" and count >= 4 else 0
                    )
                    if result["bundle_requests"] != expected_bundles:
                        raise RuntimeError(
                            f"{label}/{count}: unexpected bundle count {result['bundle_requests']}"
                        )
                    if label != "new-new" and result["bundle_messages"]:
                        raise RuntimeError(
                            f"{label}/{count}: bundle messages reached an old peer"
                        )
                    results.append({"case": f"{label}/{count}", **result})
                    print(
                        f"{label}/{count}: {result['seconds']:.3f}s, {result['bundle_requests']} bundles, SHA-256 verified",
                        flush=True,
                    )
            (artifacts / "compatibility.json").write_text(
                json.dumps(results, indent=2) + "\n"
            )
        finally:
            relay.terminate()
            relay.wait(timeout=10)


if __name__ == "__main__":
    main()
