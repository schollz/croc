#!/usr/bin/env python3
"""Map the linked CLI inventory to a downloaded Debian Sources index.

Candidates are metadata matches, not proof of compatible APIs or dependency
versions. No dependency versions or Debian control fields are changed.
"""
import argparse
import hashlib
import json
import lzma
from pathlib import Path
import re


def paragraphs(text):
    for paragraph in text.split("\n\n"):
        fields = {}
        key = None
        for line in paragraph.splitlines():
            if line.startswith(" ") and key:
                fields[key] += " " + line.strip()
            elif ": " in line:
                key, value = line.split(": ", 1)
                fields[key] = value
        if fields.get("Extra-Source-Only") != "yes":
            yield fields


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--sources", type=Path, required=True)
    parser.add_argument("--date", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    directory = Path(__file__).resolve().parent
    modules = json.loads((directory / "cli-dependencies.json").read_text())
    sources = list(paragraphs(lzma.open(args.sources, "rt").read()))
    rows = []
    for module in modules:
        path = module["path"]
        base = re.sub(r"/v[2-9][0-9]*$", "", path)
        matches = []
        for source in sources:
            imports = source.get("Go-Import-Path", "").replace(",", " ").split()
            if path in imports or base in imports:
                matches.append({
                    "source": source["Package"], "version": source["Version"],
                    "binaryPackages": source.get("Binary", "").split(", "),
                    "importPaths": imports,
                    "sourceRepository": source.get("Vcs-Browser"),
                })
        rows.append({**module, "debianCandidates": matches,
                     "status": "compare versions and APIs" if matches else "no exact import metadata match"})
    result = {
        "checked": args.date,
        "index": "https://deb.debian.org/debian/dists/sid/main/source/Sources.xz",
        "indexSHA256": hashlib.sha256(args.sources.read_bytes()).hexdigest(),
        "compiler": [{"source": s["Package"], "version": s["Version"]} for s in sources
                     if s.get("Package") in ("golang-defaults", "golang-1.27")],
        "modules": rows,
    }
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    matched = sum(bool(row["debianCandidates"]) for row in rows)
    print(f"{matched}/{len(rows)} modules have metadata candidates; all require compatibility review")


if __name__ == "__main__":
    main()
