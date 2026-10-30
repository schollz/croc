#!/usr/bin/env python3
"""Remote helper for benchmark-bundles-wan.py; receive requests over SSH stdin."""

from bundle_bench_common import manifest, stop_process, drain_output
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parent


def main():
    request = json.load(sys.stdin)
    run_id = request["id"]
    assert run_id.replace("-", "").replace("_", "").isalnum()
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("CROC_") or key.upper() in (
            "HTTP_PROXY",
            "HTTPS_PROXY",
            "ALL_PROXY",
            "SOCKS5_PROXY",
        ):
            env.pop(key)
    config = ROOT / "config"
    config.mkdir(exist_ok=True)
    env["CROC_CONFIG_DIR"] = str(config)
    env["CROC_SECRET"] = request["secret"]
    (config / "version-check.json").write_text(
        json.dumps(
            {
                "checked_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                "latest_version": "11.5.5",
            }
        )
    )
    work = ROOT / "work" / run_id
    work.mkdir(parents=True)
    logpath = ROOT / "logs" / (run_id + ".log")
    command = [request["binary"]] + request["args"]
    start = time.monotonic()
    with logpath.open("wb") as logfile:
        process = subprocess.Popen(
            command,
            cwd=work,
            env=env,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        drain = drain_output(process.stdout, logfile)
        print(json.dumps({"event": "started", "pid": process.pid}), flush=True)
        try:
            code = process.wait(timeout=request.get("timeout", 900))
        except subprocess.TimeoutExpired:
            stop_process(process, group=True)
            code = 124
        drain.join()
    duration = time.monotonic() - start
    print(
        json.dumps(
            {
                "event": "finished",
                "returncode": code,
                "seconds": duration,
                "log": logpath.read_text(errors="replace")[-15000:],
            }
        ),
        flush=True,
    )


if __name__ == "__main__":
    main()
