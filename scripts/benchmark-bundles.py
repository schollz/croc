#!/usr/bin/env python3
"""Paired Linux benchmarks on an isolated, real TCP path with fixed RTT.

Requires root, iproute2, two native binaries, and enough disk for one received
fixture. Network namespaces and qdiscs are private to this run. Public routing
and host qdiscs are never modified. Results include independent SHA-256 checks,
temporary-storage samples, socket counts, and the original debug logs.
"""

from bundle_bench_common import manifest, sha256, transfer_secret, stop_process
import argparse
import json
import os
from pathlib import Path
import secrets
import shutil
import statistics
import subprocess
import threading
import time


def command(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def drain_log(stream, logfile, started, markers):
    # Timestamp existing debug events when pipe bytes arrive. read1 drains
    # available bytes immediately instead of waiting to fill a 64 KiB buffer.
    needles = {
        "metadata_ready": b"startup milestone file-metadata-ready",
        "first_data": b"startup milestone first-decrypted-data-byte",
        "data_complete": b"finished receiving!",
        "sender_start": b"beginning sending comms",
        "sender_complete": b"closing file",
    }
    tail = b""
    while block := stream.read1(64 << 10):
        observed = time.monotonic() - started
        combined = tail + block
        for name, needle in needles.items():
            if name not in markers and needle in combined:
                markers[name] = observed
        tail = combined[-128:]
        logfile.write(block)


class Monitor(threading.Thread):
    def __init__(self, sender_temp, receiver_temp, destination):
        super().__init__(daemon=True)
        self.sender_temp, self.receiver_temp, self.destination = (
            sender_temp,
            receiver_temp,
            destination,
        )
        self.stopping = threading.Event()
        self.sender_peak = self.receiver_peak = 0
        self.samples = 0
        self.pid = None
        self.connections_peak = 0

    @staticmethod
    def sizes(paths):
        total = 0
        for path in paths:
            try:
                if path.is_file():
                    total += path.stat().st_size
            except FileNotFoundError:
                pass
        return total

    def run(self):
        while not self.stopping.is_set():
            self.sender_peak = max(
                self.sender_peak,
                self.sizes(self.sender_temp.glob("croc-bundle-send-*")),
            )
            staged = (
                file
                for directory in self.destination.glob(".croc-bundle-*")
                for file in directory.rglob("*")
            )
            received = self.sizes(
                self.receiver_temp.glob("croc-bundle-receive-*")
            ) + self.sizes(staged)
            self.receiver_peak = max(self.receiver_peak, received)
            self.samples += 1
            if self.pid and self.samples % 20 == 0:
                try:
                    sockets = (
                        Path(f"/proc/{self.pid}/net/tcp").read_text().splitlines()[1:]
                    )
                    connections = sum(
                        line.split()[3] == "01"
                        and 32171 <= int(line.split()[2].split(":")[1], 16) <= 32178
                        for line in sockets
                    )
                    self.connections_peak = max(self.connections_peak, connections)
                except FileNotFoundError:
                    pass
            self.stopping.wait(0.005)


def transfer(
    args,
    binary,
    source,
    expected,
    send_namespace,
    receive_namespace,
    output,
    relay,
    zipped=False,
):
    output.mkdir()
    destination = output / "received"
    destination.mkdir()
    sender_temp, receiver_temp = output / "sender-temp", output / "receiver-temp"
    sender_temp.mkdir()
    receiver_temp.mkdir()
    secret = transfer_secret()
    common = [
        "--debug",
        "--relay",
        relay,
        "--relay6",
        "",
        "--yes",
        "--overwrite",
        "--ignore-stdin",
        "--disable-clipboard",
    ]
    if source.is_file():
        common.append("--no-compress")
    send = [
        "ip",
        "netns",
        "exec",
        send_namespace,
        binary,
        *common,
        "send",
        "--no-local",
        "--transport",
        "relay",
    ]
    if zipped:
        send.append("--zip")
    send.append(source)
    receive = [
        "ip",
        "netns",
        "exec",
        receive_namespace,
        binary,
        *common,
        "--out",
        destination,
    ]
    monitor = Monitor(sender_temp, receiver_temp, destination)
    processes = []
    monitor.start()
    started = time.monotonic()
    try:
        drains = []
        timing_markers = {}
        # A real terminal/pipe cannot be fsynced. Giving croc a regular log file
        # makes progressbar fsync every render and measures the log disk instead.
        with (output / "sender.log").open("wb") as send_log, (
            output / "receiver.log"
        ).open("wb") as receive_log:
            try:
                for name, cmd, log, temp in [
                    ("sender", send, send_log, sender_temp),
                    ("receiver", receive, receive_log, receiver_temp),
                ]:
                    env = dict(
                        os.environ,
                        CROC_SECRET=secret,
                        CROC_CONFIG_DIR=str(output / f"config-{name}"),
                        TMPDIR=str(temp),
                    )
                    processes.append(
                        subprocess.Popen(
                            [str(arg) for arg in cmd],
                            env=env,
                            cwd=output,
                            stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT,
                        )
                    )
                    timing_markers[name] = {}
                    drain = threading.Thread(
                        target=drain_log,
                        args=(processes[-1].stdout, log, started, timing_markers[name]),
                        daemon=True,
                    )
                    drain.start()
                    drains.append(drain)
                    if name == "sender":
                        monitor.pid = processes[-1].pid
                        time.sleep(0.15)
                for process in reversed(processes):
                    if process.wait(timeout=args.timeout) != 0:
                        raise RuntimeError(f"Transfer failed; inspect {output}")
            finally:
                for process in processes:
                    stop_process(process)
                for drain in drains:
                    drain.join()
        elapsed = time.monotonic() - started
        monitor.stopping.set()
        monitor.join()
        actual = manifest(destination / source.name)
        if actual != expected:
            raise RuntimeError(
                f"Independent pathname, size, or SHA-256 mismatch: {output}"
            )
        if monitor.sender_peak > 32 << 20 or monitor.receiver_peak > 64 << 20:
            raise RuntimeError(f"Bundle storage exceeded its bound: {output}")
        leaked = (
            list(sender_temp.glob("croc-bundle-*"))
            + list(receiver_temp.glob("croc-bundle-*"))
            + list(destination.rglob(".croc-bundle-*"))
        )
        if leaked:
            raise RuntimeError(f"Bundle temporary files leaked: {leaked}")
        send_log = (output / "sender.log").read_text()
        result = {
            "seconds": elapsed,
            "binary": str(binary),
            "files": len(actual),
            "bytes": sum(file["size"] for file in actual.values()),
            "zip": zipped,
            "bundle_requests": send_log.count("read bundle-request message"),
            "sender_peak_bundle_storage": monitor.sender_peak,
            "receiver_peak_bundle_storage": monitor.receiver_peak,
            "storage_samples": monitor.samples,
            "independent_sha256": True,
            "established_data_connections_peak": monitor.connections_peak,
            "timing_markers": timing_markers,
            "bundle_timing": [
                line for line in send_log.splitlines() if "bundle timing" in line
            ]
            + [
                line
                for line in (output / "receiver.log").read_text().splitlines()
                if "bundle timing" in line
            ],
        }
        receive_markers = timing_markers["receiver"]
        if (
            source.is_file()
            and "first_data" in receive_markers
            and "data_complete" in receive_markers
        ):
            result["data_seconds"] = (
                receive_markers["data_complete"] - receive_markers["first_data"]
            )
            result["data_MBps"] = result["bytes"] / result["data_seconds"] / 1e6
        (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
        # Every trial begins with an empty destination; keep evidence, not copies
        # of completed video payloads. This also bounds total benchmark storage.
        shutil.rmtree(destination)
        return result
    finally:
        monitor.stopping.set()
        monitor.join()
        for process in processes:
            stop_process(process)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", type=Path, required=True)
    parser.add_argument("--new", type=Path, required=True)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--source-dir", type=Path)
    parser.add_argument("--fixtures-dir", type=Path)
    parser.add_argument("--video", type=Path)
    parser.add_argument(
        "--datasets",
        nargs="+",
        choices=["small", "source", "video"],
        default=["small", "source", "video"],
    )
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--rtt-ms", type=float, default=75)
    parser.add_argument("--timeout", type=int, default=300)
    parser.add_argument(
        "--resume",
        action="store_true",
        help="Reuse verified cases only when binaries and frozen inputs still match",
    )
    parser.add_argument(
        "--group",
        help="Repeat one dataset/direction/mode group while retaining every frozen input check",
    )
    args = parser.parse_args()
    args.artifacts = args.artifacts.resolve()
    args.artifacts.mkdir(parents=True, exist_ok=True)
    binary_checks = {str(path): sha256(path) for path in [args.base, args.new]}
    binary_record = args.artifacts / "binary-sha256.json"
    if (
        binary_record.exists()
        and json.loads(binary_record.read_text()) != binary_checks
    ):
        raise RuntimeError("Benchmark binaries changed; use a fresh artifact directory")
    binary_record.write_text(json.dumps(binary_checks, indent=2))
    fixtures = (
        args.fixtures_dir.resolve()
        if args.fixtures_dir
        else args.artifacts / "fixtures"
    )
    fixtures.mkdir(exist_ok=True)
    small = fixtures / "small-256"
    small.mkdir(exist_ok=True)
    for index in range(256):
        fixture = small / f"{index:04}.bin"
        if not fixture.exists():
            fixture.write_bytes(
                bytes(
                    (position * 31 + index) % 251
                    for position in range(1024 + index % 16)
                )
            )
    sources = {"small": small}
    for name, source in [("source", args.source_dir), ("video", args.video)]:
        if name not in args.datasets:
            continue
        if not source:
            parser.error(f"--{name if name == 'video' else 'source-dir'} is required")
        target = fixtures / ("video.bin" if name == "video" else "source")
        if not target.exists():
            if source.is_dir():
                shutil.copytree(source, target)
            else:
                shutil.copy2(source, target)
        sources[name] = target
    manifests = {name: manifest(source) for name, source in sources.items()}
    input_record = args.artifacts / "input-manifests.json"
    if input_record.exists() and json.loads(input_record.read_text()) != manifests:
        raise RuntimeError("Frozen benchmark inputs changed")
    input_record.write_text(json.dumps(manifests, indent=2))
    identity = secrets.token_hex(3)
    left, right = f"croc-bundle-{identity}-a", f"croc-bundle-{identity}-b"
    link_a, link_b = f"cb{identity}a", f"cb{identity}b"
    created = []
    relay = None
    results = []
    try:
        for namespace in [left, right]:
            command("ip", "netns", "add", namespace)
            created.append(namespace)
            command("ip", "-n", namespace, "link", "set", "lo", "up")
        command("ip", "link", "add", link_a, "type", "veth", "peer", "name", link_b)
        for namespace, link, address in [
            (left, link_a, "10.203.0.1/24"),
            (right, link_b, "10.203.0.2/24"),
        ]:
            command("ip", "link", "set", link, "netns", namespace)
            command("ip", "-n", namespace, "addr", "add", address, "dev", link)
            command("ip", "-n", namespace, "link", "set", link, "up")
            command(
                "ip",
                "netns",
                "exec",
                namespace,
                "tc",
                "qdisc",
                "add",
                "dev",
                link,
                "root",
                "netem",
                "delay",
                f"{args.rtt_ms / 2}ms",
            )
        ping = subprocess.check_output(
            ["ip", "netns", "exec", left, "ping", "-c", "5", "10.203.0.2"], text=True
        )
        (args.artifacts / "controlled-path.txt").write_text(ping)
        relay_log = (args.artifacts / "relay.log").open("w")
        relay = subprocess.Popen(
            [
                "ip",
                "netns",
                "exec",
                right,
                str(args.new),
                "--debug",
                "relay",
                "--host",
                "0.0.0.0",
                "--ports",
                "32170,32171,32172,32173,32174,32175,32176,32177,32178",
                "--source-join-limit",
                "10000",
                "--room-join-limit",
                "10000",
            ],
            stdout=relay_log,
            stderr=subprocess.STDOUT,
        )
        time.sleep(0.5)
        for dataset in args.datasets:
            source = sources[dataset]
            for direction, sender, receiver in [
                ("up", left, right),
                ("down", right, left),
            ]:
                for zipped in ([False, True] if dataset == "source" else [False]):
                    if (
                        args.group
                        and args.group
                        != f"{dataset}/{direction}/{'zip' if zipped else 'raw'}"
                    ):
                        continue
                    for trial in range(args.repeats):
                        profiles = [("base", args.base), ("new", args.new)]
                        if trial % 2:
                            profiles.reverse()
                        for label, binary in profiles:
                            case = f"{dataset}-{direction}-{'zip' if zipped else 'raw'}-{trial}-{label}"
                            output = args.artifacts / case
                            if args.resume and (output / "result.json").exists():
                                result = json.loads(
                                    (output / "result.json").read_text()
                                )
                                result.update(
                                    dataset=dataset,
                                    direction=direction,
                                    trial=trial,
                                    profile=label,
                                )
                                results.append(result)
                                continue
                            if args.resume and output.exists():
                                output.rename(
                                    args.artifacts
                                    / (case + f"-failed-{time.time_ns()}")
                                )
                            result = transfer(
                                args,
                                binary,
                                source,
                                manifests[dataset],
                                sender,
                                receiver,
                                output,
                                "10.203.0.2:32170",
                                zipped,
                            )
                            result.update(
                                dataset=dataset,
                                direction=direction,
                                trial=trial,
                                profile=label,
                            )
                            results.append(result)
                            with (args.artifacts / "results.jsonl").open("a") as stream:
                                stream.write(json.dumps(result) + "\n")
                            print(
                                f"{case}: {result['seconds']:.3f}s, {result['bundle_requests']} bundles, SHA-256 verified",
                                flush=True,
                            )
        summary = {}
        for result in results:
            key = f"{result['dataset']}/{result['direction']}/{'zip' if result['zip'] else 'raw'}/{result['profile']}"
            summary.setdefault(key, []).append(result["seconds"])
        summary = {
            key: {
                "median": statistics.median(times),
                "min": min(times),
                "max": max(times),
                "trials": len(times),
            }
            for key, times in summary.items()
        }
        (args.artifacts / "summary.json").write_text(
            json.dumps(summary, indent=2) + "\n"
        )
        print(json.dumps(summary, indent=2))
    finally:
        if relay:
            relay.terminate()
            relay.wait(timeout=10)
        for namespace in reversed(created):
            subprocess.run(["ip", "netns", "del", namespace], check=False)
        subprocess.run(
            ["ip", "link", "del", link_a],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            check=False,
        )


if __name__ == "__main__":
    main()
