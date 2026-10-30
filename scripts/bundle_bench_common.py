"""Shared plumbing for bundle benchmarks and frozen-client compatibility.

Deploy this file beside bundle_remote_runner.py when orchestrating a WAN run.
"""

import hashlib
import os
import secrets
import shutil
import signal
import subprocess
import threading

_USED_ROOMS = set()


def transfer_secret():
    # Old peers use the first four characters as the relay room. Do not reuse
    # one while preceding sessions may still be closing.
    while True:
        room = secrets.token_hex(2)
        if room not in _USED_ROOMS:
            _USED_ROOMS.add(room)
            return room + "-" + secrets.token_hex(16)


def sha256(file):
    with file.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def manifest(source):
    files = (
        [source]
        if source.is_file()
        else sorted(file for file in source.rglob("*") if file.is_file())
    )
    return {
        (source.name if source.is_file() else file.relative_to(source).as_posix()): {
            "size": file.stat().st_size,
            "sha256": sha256(file),
        }
        for file in files
    }


def stop_process(process, *, group=False):
    if process.poll() is not None:
        return
    try:
        if group:
            os.killpg(process.pid, signal.SIGTERM)
        else:
            process.terminate()
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        try:
            if group:
                os.killpg(process.pid, signal.SIGKILL)
            else:
                process.kill()
        except ProcessLookupError:
            pass
        process.wait()


def drain_output(stream, logfile):
    drain = threading.Thread(
        target=shutil.copyfileobj, args=(stream, logfile), daemon=True
    )
    drain.start()
    return drain
