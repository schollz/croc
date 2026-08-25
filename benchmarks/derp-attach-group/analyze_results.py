#!/usr/bin/env python3

import argparse
import csv
import json
import math
import pathlib
import random
import re
import statistics


def percentile(values, fraction):
    return values[min(int(len(values) * fraction), len(values) - 1)]


def bootstrap_ratio(log_ratios, seed, resamples):
    if not log_ratios:
        return {
            "pairs": 0,
            "geometric_mean": None,
            "ci95_low": None,
            "ci95_high": None,
        }
    rng = random.Random(seed)
    means = sorted(
        sum(rng.choice(log_ratios) for _ in log_ratios) / len(log_ratios)
        for _ in range(resamples)
    )
    return {
        "pairs": len(log_ratios),
        "geometric_mean": math.exp(sum(log_ratios) / len(log_ratios)),
        "ci95_low": math.exp(percentile(means, 0.025)),
        "ci95_high": math.exp(percentile(means, 0.975)),
    }


def extract_number(pattern, text):
    match = re.search(pattern, text, flags=re.MULTILINE)
    return float(match.group(1)) if match else 0.0


def parse_go_duration(value):
    if not value:
        return 0.0
    units = {
        "ns": 1e-9,
        "us": 1e-6,
        "µs": 1e-6,
        "ms": 1e-3,
        "s": 1.0,
        "m": 60.0,
        "h": 3600.0,
    }
    parts = list(re.finditer(r"([0-9]+(?:\.[0-9]+)?)(ns|us|µs|ms|s|m|h)", value))
    if not parts or "".join(part.group(0) for part in parts) != value:
        return 0.0
    return sum(float(part.group(1)) * units[part.group(2)] for part in parts)


def summary_value(text, name):
    matches = re.findall(rf"\b{re.escape(name)}=([^\s]*)", text)
    return matches[-1] if matches else ""


def resource_stats(row, logs):
    label = f"{row['direction']}-p{row['pair']}-s{row['sequence']}-{row['variant']}"
    if row["direction"] == "l2r":
        local = logs / f"{label}-local-sender.log"
        remote = logs / f"{label}-remote-receiver.log"
    else:
        local = logs / f"{label}-local-receiver.log"
        remote = logs / f"{label}-remote-sender.log"
    local_text = local.read_text(errors="ignore")
    remote_text = remote.read_text(errors="ignore")
    local_cpu_seconds = sum(
        (
            extract_number(r"^user\s+([0-9.]+)$", local_text),
            extract_number(r"^sys\s+([0-9.]+)$", local_text),
            extract_number(r"\s([0-9.]+) user\s", local_text),
            extract_number(r"\s([0-9.]+) sys(?:\s|$)", local_text),
        )
    )
    remote_cpu_seconds = sum(
        (
            extract_number(r"User time \(seconds\):\s*([0-9.]+)", remote_text),
            extract_number(r"System time \(seconds\):\s*([0-9.]+)", remote_text),
        )
    )
    local_rss_bytes = extract_number(
        r"^\s*([0-9.]+)\s+maximum resident set size$", local_text
    )
    remote_rss_kib = extract_number(
        r"Maximum resident set size \(kbytes\):\s*([0-9.]+)", remote_text
    )
    setup_seconds = max(
        parse_go_duration(summary_value(local_text, "setup")),
        parse_go_duration(summary_value(remote_text, "setup")),
    )
    raw_setup_seconds = max(
        parse_go_duration(summary_value(local_text, "raw_setup")),
        parse_go_duration(summary_value(remote_text, "raw_setup")),
    )
    raw_paths = max(
        int(summary_value(local_text, "raw_paths") or 0),
        int(summary_value(remote_text, "raw_paths") or 0),
    )
    return {
        "cpu_seconds": local_cpu_seconds + remote_cpu_seconds,
        "local_cpu_seconds": local_cpu_seconds,
        "remote_cpu_seconds": remote_cpu_seconds,
        "local_rss_kib": local_rss_bytes / 1024,
        "remote_rss_kib": remote_rss_kib,
        "setup_seconds": setup_seconds,
        "raw_setup_seconds": raw_setup_seconds,
        "raw_paths": raw_paths,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("result_dir", type=pathlib.Path)
    parser.add_argument(
        "--candidate",
        help="candidate variant name; inferred when results contain one non-legacy variant",
    )
    parser.add_argument("--seed", type=int, default=20260824)
    parser.add_argument("--resamples", type=int, default=100_000)
    args = parser.parse_args()

    with (args.result_dir / "results.csv").open(newline="") as source:
        rows = list(csv.DictReader(source))
    candidates = sorted({row["variant"] for row in rows} - {"legacy"})
    if args.candidate:
        candidate = args.candidate
    elif len(candidates) == 1:
        candidate = candidates[0]
    else:
        parser.error(
            "--candidate is required when results contain zero or multiple candidates"
        )
    logs = args.result_dir / "logs"
    for row in rows:
        row["wall"] = float(row["wall_seconds"])
        row["normalized"] = float(row["normalized_goodput"])
        row["mbps"] = int(row["bytes"]) * 8 / row["wall"] / 1e6
        row.update(resource_stats(row, logs))
        row["cpu_seconds_per_gib"] = row["cpu_seconds"] * (1024**3) / int(
            row["bytes"]
        )

    output = {"directions": {}, "paired": {}}
    paired_logs = []
    raw_paired_logs = []
    for direction in ("l2r", "r2l"):
        direction_rows = [row for row in rows if row["direction"] == direction]
        output["directions"][direction] = {}
        for variant in ("legacy", candidate):
            selected = [row for row in direction_rows if row["variant"] == variant]
            if not selected:
                continue
            output["directions"][direction][variant] = {
                "runs": len(selected),
                "sha256_valid": sum(row["sha_ok"] == "true" for row in selected),
                "raw_direct_runs": sum(
                    row["mode_proof"] == "raw-direct-2of2" for row in selected
                ),
                "median_wall_seconds": statistics.median(row["wall"] for row in selected),
                "median_goodput_mbps": statistics.median(row["mbps"] for row in selected),
                "median_normalized_goodput": statistics.median(
                    row["normalized"] for row in selected
                ),
                "median_setup_seconds": statistics.median(
                    row["setup_seconds"] for row in selected
                ),
                "p95_setup_seconds": percentile(
                    sorted(row["setup_seconds"] for row in selected), 0.95
                ),
                "median_raw_setup_seconds": statistics.median(
                    row["raw_setup_seconds"] for row in selected
                ),
                "median_raw_paths": statistics.median(
                    row["raw_paths"] for row in selected
                ),
                "median_cpu_seconds_per_gib": statistics.median(
                    row["cpu_seconds_per_gib"] for row in selected
                ),
                "median_local_rss_kib": statistics.median(
                    row["local_rss_kib"] for row in selected
                ),
                "median_remote_rss_kib": statistics.median(
                    row["remote_rss_kib"] for row in selected
                ),
                "max_remote_rss_kib": max(row["remote_rss_kib"] for row in selected),
                "max_local_rss_kib": max(row["local_rss_kib"] for row in selected),
            }

        pairs = {}
        for row in direction_rows:
            pairs.setdefault(row["pair"], {})[row["variant"]] = row
        direction_logs = []
        direction_raw_logs = []
        for pair in pairs.values():
            if set(pair) != {"legacy", candidate}:
                continue
            if pair["legacy"]["normalized"] <= 0:
                continue
            ratio = pair[candidate]["normalized"] / pair["legacy"]["normalized"]
            if ratio <= 0:
                continue
            log_ratio = math.log(ratio)
            direction_logs.append(log_ratio)
            paired_logs.append(log_ratio)
            if pair[candidate]["mode_proof"] == "raw-direct-2of2":
                direction_raw_logs.append(log_ratio)
                raw_paired_logs.append(log_ratio)
        output["paired"][direction] = bootstrap_ratio(
            direction_logs, args.seed, args.resamples
        )
        output["paired"][f"{direction}_raw_only"] = bootstrap_ratio(
            direction_raw_logs, args.seed, args.resamples
        )

    output["paired"]["combined"] = bootstrap_ratio(
        paired_logs, args.seed, args.resamples
    )
    output["paired"]["combined_raw_only"] = bootstrap_ratio(
        raw_paired_logs, args.seed, args.resamples
    )
    output["bootstrap"] = {"seed": args.seed, "resamples": args.resamples}
    output["candidate"] = candidate
    print(json.dumps(output, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
