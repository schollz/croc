#!/usr/bin/env python3
"""Inspect all Linux packages and transfer through a local relay (Linux/QEMU)."""
import argparse
import gzip
import io
import os
from pathlib import Path, PurePosixPath
import platform
import shutil
import socket
import stat
import subprocess
import tarfile
import tempfile
import time

from release import TARGETS, digest, package_name, unpack_binary, version

FILES = {
    "usr/bin/croc", "usr/share/doc/croc/LICENSE", "usr/share/doc/croc/THIRD_PARTY_NOTICES.md",
    "usr/share/doc/croc/wordlists-LICENSE.txt", "usr/share/doc/croc/README.md",
    "usr/share/doc/croc/copyright", "usr/share/doc/croc/changelog.Debian.gz",
    "usr/share/doc/croc/THIRD_PARTY_LICENSES.txt",
    "usr/share/man/man1/croc.1.gz", "usr/share/bash-completion/completions/croc",
    "usr/share/zsh/site-functions/_croc", "usr/share/fish/vendor_completions.d/croc.fish",
}
DIRECTORIES = {str(parent) for name in FILES for parent in PurePosixPath(name).parents}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def command(*args, **kwargs):
    return subprocess.check_output(args, timeout=120, text=True, **kwargs).strip()


def extract_payload(payload, path, destination):
    with tarfile.open(fileobj=io.BytesIO(payload)) as archive:
        actual = set()
        for member in archive:
            name = member.name.removeprefix("./").rstrip("/") or "."
            require(member.uid == member.gid == 0, f"{path}: non-root owner: {name}")
            if member.isdir():
                require(name in DIRECTORIES, f"{path}: unexpected directory: {name}")
                require(member.mode == 0o755, f"{path}: directory permissions: {name}")
                continue
            require(member.isfile() and name in FILES, f"{path}: unexpected file: {name}")
            require(name not in actual, f"{path}: duplicate file: {name}")
            require(member.mode == (0o755 if name == "usr/bin/croc" else 0o644), f"{path}: permissions: {name}")
            actual.add(name)
        require(actual == FILES, f"{path}: incomplete payload: {FILES-actual}")
        archive.extractall(destination, filter="data")


def inspect_package(path, target, v, destination):
    if path.suffix == ".deb":
        fields = command("dpkg-deb", "-f", str(path), "Package", "Version", "Architecture", "Depends")
        for expected in ("Package: croc", f"Version: {v}-1", f"Architecture: {target['deb']}", "Depends: ca-certificates"):
            require(expected in fields.splitlines(), f"{path}: missing {expected}: {fields}")
        control = subprocess.check_output(["dpkg-deb", "--ctrl-tarfile", str(path)])
        with tarfile.open(fileobj=io.BytesIO(control)) as archive:
            require(all(m.name.removeprefix("./") in ("", "control", "md5sums", "conffiles") for m in archive),
                    f"{path}: unexpected maintainer script")
        payload = subprocess.check_output(["dpkg-deb", "--fsys-tarfile", str(path)])
    else:
        command("rpm", "-K", "--nosignature", str(path))
        metadata = command("rpm", "-qp", "--qf", "%{NAME}\n%{VERSION}\n%{RELEASE}\n%{ARCH}\n%{LICENSE}", str(path))
        require(metadata.splitlines() == ["croc", v, "1", target["rpm"], "MIT"], f"{path}: {metadata}")
        require("ca-certificates" in command("rpm", "-qp", "--requires", str(path)).splitlines(), f"{path}: no CA dependency")
        require(not command("rpm", "-qp", "--scripts", str(path)), f"{path}: unexpected install script")
        listing = command("rpm", "-qp", "--qf", "[%{FILENAMES}\t%{FILEMODES}\t%{FILEUSERNAME}\t%{FILEGROUPNAME}\n]", str(path))
        actual = set()
        for line in listing.splitlines():
            name, mode, user, group = line.split("\t")
            name = name.lstrip("/")
            mode = int(mode)
            require(user == group == "root", f"{path}: owner: {name}")
            if stat.S_ISDIR(mode):
                require(stat.S_IMODE(mode) == 0o755, f"{path}: directory permissions: {name}")
                continue
            require(stat.S_ISREG(mode) and name in FILES, f"{path}: unexpected file: {name}")
            require(stat.S_IMODE(mode) == (0o755 if name == "usr/bin/croc" else 0o644), f"{path}: permissions: {name}")
            actual.add(name)
        require(actual == FILES, f"{path}: incomplete payload: {FILES-actual}")
        with path.open("rb") as package:
            payload = subprocess.check_output(["rpm2archive", "-n", "-"], stdin=package, timeout=120)
    extract_payload(payload, path, destination)
    binary = destination / "usr/bin/croc"
    require("INTERP" not in command("readelf", "-l", str(binary)), f"{path}: not static")
    docs = destination / "usr/share/doc/croc"
    require("MIT License" in (docs / "LICENSE").read_text(), f"{path}: MIT notice missing")
    require((docs / "THIRD_PARTY_NOTICES.md").stat().st_size > 1000, f"{path}: dependency notices missing")
    notices = (docs / "THIRD_PARTY_LICENSES.txt").read_text()
    require("Go standard library" in notices and "tailscale.com@" in notices and "gvisor.dev/gvisor@" in notices,
            f"{path}: bundled module license notices missing")
    require(gzip.decompress((destination / "usr/share/man/man1/croc.1.gz").read_bytes()).startswith(b".TH CROC 1"), f"{path}: manual missing")
    zsh = destination / "usr/share/zsh/site-functions/_croc"
    require("#compdef croc" in zsh.read_text() and "$PROG" not in zsh.read_text(), f"{path}: incorrect zsh command")
    fish = destination / "usr/share/fish/vendor_completions.d/croc.fish"
    require("complete -c croc" in fish.read_text(), f"{path}: incorrect fish command")
    for shell, script in (("bash", destination / "usr/share/bash-completion/completions/croc"), ("zsh", zsh), ("fish", fish)):
        if shutil.which(shell):
            command(shell, "-n", str(script))
    return binary


def executable(binary, target):
    native = {"x86_64": "amd64", "aarch64": "arm64", "riscv64": "riscv64"}.get(platform.machine())
    if native == target["goarch"] and not target.get("cpu"):
        return [str(binary)]
    qemu = shutil.which(f"qemu-{target['qemu']}")
    require(qemu, f"qemu-{target['qemu']} required for {target['name']}")
    return [qemu, *(["-cpu", target["cpu"]] if target.get("cpu") else []), str(binary)]


def stop(process):
    if process.poll() is None:
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()


def transfer(sender, receiver, work, port, directory=False):
    work.mkdir()
    send_dir, recv_dir = work / "send", work / "receive"
    send_dir.mkdir()
    recv_dir.mkdir()
    source = send_dir / ("folder" if directory else "payload.bin")
    if directory:
        source.mkdir()
        (source / "nested").mkdir()
        (source / "nested/data.bin").write_bytes(os.urandom(65537))
        (source / "space name.txt").write_text("croc package directory test\n")
    else:
        source.write_bytes(os.urandom(262147))
    common = ["--yes", "--ignore-stdin", "--disable-clipboard", "--relay", f"127.0.0.1:{port}", "--relay6", ""]
    # Three lowercase words select a distinct room using the entire first word.
    # Changing only the suffix of a legacy 1234-... code reuses the same room.
    selector = "".join(chr(ord("a") + byte % 26) for byte in os.urandom(16))
    env = dict(os.environ, CROC_SECRET=f"{selector}-package-validation", CROC_CONFIG_DIR=str(work / "config"))
    with (work / "sender.log").open("w+") as log:
        process = subprocess.Popen([*sender, *common, "send", "--transport", "relay", "--no-local", str(source)],
                                   cwd=send_dir, env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
        try:
            received = subprocess.run([*receiver, *common, "--out", str(recv_dir)], env=env,
                                      stdin=subprocess.DEVNULL, capture_output=True, timeout=120, text=True)
            status = process.wait(timeout=120)
            log.seek(0)
            require(received.returncode == status == 0, f"transfer failed: {log.read()}\n{received.stdout}\n{received.stderr}")
            originals = sorted(p for p in send_dir.rglob("*") if p.is_file())
            require(bool(originals), "empty test payload")
            for original in originals:
                copied = recv_dir / original.relative_to(send_dir)
                require(copied.is_file() and digest(copied) == digest(original), f"hash mismatch: {copied}")
        finally:
            stop(process)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--artifacts", type=Path, required=True)
    parser.add_argument("--version", type=version, required=True)
    args = parser.parse_args()
    artifacts = args.artifacts.resolve()
    with tempfile.TemporaryDirectory(prefix="croc-verify-") as temp:
        work = Path(temp)
        binaries = {}
        for target in TARGETS:
            archive_binary = work / target["name"]
            unpack_binary(artifacts / f"croc_v{args.version}_{target['name']}.tar.gz", archive_binary)
            for fmt in ("deb", "rpm"):
                package = artifacts / package_name(args.version, target, fmt)
                binary = inspect_package(package, target, args.version, work / f"{target['name']}-{fmt}")
                require(digest(binary) == digest(archive_binary), f"{package}: binary differs from archive")
                invoke = executable(binary, target)
                require(command(*invoke, "--version") == f"croc version {args.version}", f"{package}: version mismatch")
                require("send" in command(*invoke, "--help"), f"{package}: help missing")
                binaries[target["name"], fmt] = invoke
                print(f"Inspected and executed {package.name}", flush=True)
        native = next((t for t in TARGETS if t["goarch"] == {"aarch64": "arm64", "x86_64": "amd64"}.get(platform.machine())), TARGETS[0])
        relay_command = binaries[native["name"], "deb"]
        port = 19009
        with (work / "relay.log").open("w+") as log:
            relay = subprocess.Popen([*relay_command, "relay", "--host", "127.0.0.1", "--ports", f"{port},{port+1}"], stdout=log, stderr=log)
            try:
                for attempt in range(100):
                    try:
                        with socket.create_connection(("127.0.0.1", port), timeout=0.1):
                            break
                    except OSError:
                        require(relay.poll() is None, "relay exited")
                        time.sleep(0.1)
                else:
                    raise RuntimeError("local relay did not start")
                for index, target in enumerate(TARGETS):
                    other = TARGETS[(index+1) % len(TARGETS)]
                    transfer(binaries[target["name"], "deb"], binaries[other["name"], "rpm"], work / f"transfer-{index}", port)
                    print(f"Transfer/hash OK: {target['name']} DEB -> {other['name']} RPM", flush=True)
                transfer(binaries[native["name"], "rpm"], binaries[native["name"], "deb"], work / "directory", port, directory=True)
                print("Directory transfer/hash OK", flush=True)
            finally:
                stop(relay)


if __name__ == "__main__":
    main()
