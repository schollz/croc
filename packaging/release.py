#!/usr/bin/env python3
"""Build and verify croc release packages without publishing anything."""
import argparse
from datetime import datetime, timezone
from email.utils import format_datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parent.parent
TARGETS = json.loads((ROOT / "packaging/linux-targets.json").read_text())
OTHER_ARCHIVES = {
    "Windows-64bit": "zip", "Windows-32bit": "zip", "Windows-ARM64": "zip",
    "macOS-64bit": "tar.gz", "macOS-ARM64": "tar.gz",
    "DragonFlyBSD-64bit": "tar.gz", "FreeBSD-64bit": "tar.gz",
    "FreeBSD-ARM64": "tar.gz", "NetBSD-32bit": "tar.gz",
    "NetBSD-64bit": "tar.gz", "NetBSD-ARM64": "tar.gz",
    "OpenBSD-64bit": "tar.gz", "OpenBSD-ARM64": "tar.gz",
}


def run(*args, cwd=ROOT, env=None, capture=False):
    return subprocess.run(args, cwd=cwd, env=env, check=True,
                          stdout=subprocess.PIPE if capture else None,
                          text=capture).stdout


def version(value):
    value = value.removeprefix("v")
    if not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", value):
        raise ValueError("expected a stable MAJOR.MINOR.PATCH version")
    return value


def package_name(v, target, fmt):
    if fmt == "deb":
        return f"croc_{v}-1_{target['deb']}.deb"
    return f"croc-{v}-1.{target['rpm']}.rpm"


def expected_assets(v):
    names = {f"croc_v{v}_{name}.{ext}" for name, ext in OTHER_ARCHIVES.items()}
    names.update(f"croc_v{v}_{t['name']}.tar.gz" for t in TARGETS)
    names.update(package_name(v, t, fmt) for t in TARGETS for fmt in ("deb", "rpm"))
    names.update((f"croc-web_v{v}_Linux-amd64.tar.gz", f"croc_v{v}_src.tar.gz",
                  f"croc_v{v}_checksums.txt"))
    return names


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def check_assets(directory, v, write=False):
    checksum = f"croc_v{v}_checksums.txt"
    expected = expected_assets(v)
    actual = {p.name for p in directory.iterdir() if p.is_file()}
    if write:
        actual.add(checksum)
    if actual != expected:
        raise ValueError(f"release assets mismatch; missing={sorted(expected-actual)}, "
                         f"unexpected={sorted(actual-expected)}")
    lines = [f"{digest(directory / name)}  {name}\n" for name in sorted(expected - {checksum})]
    content = "".join(lines)
    if write:
        (directory / checksum).write_text(content)
    elif (directory / checksum).read_text() != content:
        raise ValueError("checksums do not match the complete release payload")
    print(f"Verified {len(expected)} release assets")


def check_draft(metadata, v):
    release = json.loads(metadata.read_text())
    names = [asset["name"] for asset in release["assets"]]
    if release.get("isDraft") is not True or len(names) != 34 or set(names) != expected_assets(v):
        raise ValueError("remote draft does not contain the exact 34 release assets")


def epoch(ref="HEAD"):
    return int(os.environ.get("SOURCE_DATE_EPOCH") or run(
        "git", "show", "-s", "--format=%ct", ref, capture=True).strip())


def copy_file(source, destination, mode=0o644):
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(source, destination)
    destination.chmod(mode)


def dependency_licenses(binaries):
    modules = set()
    for line in run("go", "version", "-m", *(str(p) for p in binaries), capture=True).splitlines():
        fields = line.split()
        if fields and fields[0] == "=>":
            raise ValueError("replacement modules need an explicit license mapping")
        if len(fields) >= 3 and fields[0] == "dep":
            modules.add((fields[1], fields[2]))
    if not modules:
        raise ValueError("release binaries contain no module metadata")
    notices = ["License notices for modules recorded in the Linux release binaries.\n"]
    goroot = Path(run("go", "env", "GOROOT", capture=True).strip())
    go_license = goroot / "LICENSE"
    if not go_license.exists():
        # Homebrew keeps the distribution license alongside libexec.
        go_license = goroot.parent / "LICENSE"
    notices += ["\n=== Go standard library ===\n", go_license.read_text()]
    locks = {name: (ROOT / name).read_bytes() for name in ("go.mod", "go.sum")}
    for path, v in sorted(modules):
        info = json.loads(run("go", "mod", "download", "-json", f"{path}@{v}", capture=True,
                              env=dict(os.environ, GOTOOLCHAIN="local")))
        directory = Path(info["Dir"])
        files = sorted(p for p in directory.rglob("*") if p.is_file() and
                       ("license" in p.name.lower() or p.name.lower().startswith(("copying", "notice", "authors", "patents"))))
        if not files:
            raise ValueError(f"no license notices found for {path}@{v}")
        for file in files:
            notices.extend((f"\n=== {path}@{v}: {file.relative_to(directory)} ===\n",
                            file.read_text(errors="replace")))
    if any((ROOT / name).read_bytes() != value for name, value in locks.items()):
        raise ValueError("license preparation changed tagged module metadata")
    return "\n".join(notices)


def stage_payload(binary, destination, fish, v, licenses):
    copy_file(binary, destination / "usr/bin/croc", 0o755)
    docs = destination / "usr/share/doc/croc"
    for name in ("LICENSE", "THIRD_PARTY_NOTICES.md", "README.md"):
        copy_file(ROOT / name, docs / name)
    (docs / "THIRD_PARTY_LICENSES.txt").write_text(licenses)
    copy_file(ROOT / "src/codephrase/wordlists/LICENSE.txt", docs / "wordlists-LICENSE.txt")
    copyright_text = ((docs / "LICENSE").read_text() +
                      "\nAdditional component copyrights and license texts are supplied in\n"
                      "/usr/share/doc/croc/THIRD_PARTY_NOTICES.md and\n"
                      "/usr/share/doc/croc/THIRD_PARTY_LICENSES.txt and\n"
                      "/usr/share/doc/croc/wordlists-LICENSE.txt.\n"
                      "On Debian, the system Apache-2.0 license is /usr/share/common-licenses/Apache-2.0.\n")
    (docs / "copyright").write_text(copyright_text)
    date = format_datetime(datetime.fromtimestamp(epoch(), timezone.utc))
    changelog = (f"croc ({v}-1) unstable; urgency=medium\n\n"
                 "  * Package upstream CLI binaries, documentation, and shell completions.\n\n"
                 f" -- croc maintainers <zack.scholl+croc@gmail.com>  {date}\n")
    (docs / "changelog.Debian.gz").write_bytes(gzip.compress(changelog.encode(), mtime=0))
    manual = destination / "usr/share/man/man1/croc.1.gz"
    manual.parent.mkdir(parents=True, exist_ok=True)
    manual.write_bytes(gzip.compress((ROOT / "packaging/croc.1").read_bytes(), mtime=0))
    bash = destination / "usr/share/bash-completion/completions/croc"
    copy_file(ROOT / "src/install/bash_autocomplete", bash)
    zsh = destination / "usr/share/zsh/site-functions/_croc"
    copy_file(ROOT / "src/install/zsh_autocomplete", zsh)
    zsh.write_text(zsh.read_text().replace("#compdef $PROG", "#compdef croc").replace(
        "compdef _cli_zsh_autocomplete $PROG", "compdef _cli_zsh_autocomplete croc"))
    fish_path = destination / "usr/share/fish/vendor_completions.d/croc.fish"
    fish_path.parent.mkdir(parents=True, exist_ok=True)
    fish_path.write_text(fish)
    timestamp = epoch()
    for path in destination.rglob("*"):
        if path.is_dir():
            path.chmod(0o755)
        elif path.is_file():
            path.chmod(0o755 if path == destination / "usr/bin/croc" else 0o644)
        os.utime(path, (timestamp, timestamp))


def unpack_binary(archive, destination):
    # Read only the named regular file; never extract arbitrary archive paths.
    with tarfile.open(archive) as stream:
        matches = [m for m in stream.getmembers() if m.name == "croc" and m.isfile()]
        if len(matches) != 1:
            raise ValueError(f"{archive}: expected exactly one regular croc executable")
        with stream.extractfile(matches[0]) as source, destination.open("wb") as output:
            shutil.copyfileobj(source, output)
    destination.chmod(0o755)


def make_packages(args):
    artifacts = args.artifacts.resolve()
    out = (args.output or args.artifacts).resolve()
    out.mkdir(parents=True, exist_ok=True)
    nfpm = str(Path(args.nfpm).resolve()) if "/" in args.nfpm else args.nfpm
    pinned = (ROOT / "packaging/nfpm-version").read_text().strip().removeprefix("v")
    # Go-installed tools report their module version via go version -m.
    nfpm_path = shutil.which(nfpm) or nfpm
    info = run("go", "version", "-m", nfpm_path, capture=True)
    if f"github.com/goreleaser/nfpm/v2\tv{pinned}" not in info:
        raise ValueError(f"use nFPM v{pinned} (see packaging/nfpm-version)")
    with tempfile.TemporaryDirectory(prefix="croc-packages-") as temp:
        work = Path(temp)
        native = args.native_binary.resolve() if args.native_binary else work / "native-croc"
        if args.native_binary is None:
            unpack_binary(artifacts / f"croc_v{args.version}_Linux-64bit.tar.gz", native)
        native_env = dict(os.environ, CROC_CONFIG_DIR=str(work / "config"))
        if run(str(native), "--version", capture=True, env=native_env).strip() != f"croc version {args.version}":
            raise ValueError("native completion generator has the wrong release version")
        fish = run(str(native), "generate-fish-completion", capture=True, env=native_env)
        binaries = {}
        for target in TARGETS:
            binary = work / target["name"]
            unpack_binary(artifacts / f"croc_v{args.version}_{target['name']}.tar.gz", binary)
            binaries[target["name"]] = binary
        licenses = dependency_licenses(binaries.values())
        for target in TARGETS:
            binary = binaries[target["name"]]
            stage = work / (target["name"] + "-stage")
            stage_payload(binary, stage, fish, args.version, licenses)
            env = dict(os.environ, PACKAGE_ARCH=target["arch"], PACKAGE_VERSION=args.version,
                       PACKAGE_STAGE=str(stage), SOURCE_DATE_EPOCH=str(epoch()))
            for fmt in ("deb", "rpm"):
                final = out / package_name(args.version, target, fmt)
                temporary = final.with_name("." + final.name + ".tmp")
                try:
                    run(nfpm, "package", "--config", str(ROOT / "packaging/nfpm.yaml"),
                        "--packager", fmt, "--target", str(temporary), env=env)
                    temporary.replace(final)
                finally:
                    temporary.unlink(missing_ok=True)


def build(args):
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED="0", GOTOOLCHAIN="local")
    flags = ["-buildvcs=false", "-trimpath", "-tags=netgo,osusergo", "-ldflags=-s -w -buildid="]
    native = out / "native-croc"
    run("go", "build", *flags, "-o", str(native), ".", env=env)
    if run(str(native), "--version", capture=True).strip() != f"croc version {args.version}":
        raise ValueError("source version does not match requested version")
    for target in TARGETS:
        binary = out / target["name"] / "croc"
        binary.parent.mkdir(exist_ok=True)
        target_env = dict(env, GOOS="linux", GOARCH=target["goarch"], GOARM=target.get("goarm", ""))
        run("go", "build", *flags, "-o", str(binary), ".", env=target_env)
        archive = out / f"croc_v{args.version}_{target['name']}.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            stream.add(binary, arcname="croc")
            for name in ("LICENSE", "THIRD_PARTY_NOTICES.md", "src/codephrase/wordlists/LICENSE.txt"):
                stream.add(ROOT / name, arcname=name)
        print(f"Built {target['name']}", flush=True)


def source_archive(args):
    out = args.output.resolve()
    out.parent.mkdir(parents=True, exist_ok=True)
    commit = run("git", "rev-parse", f"{args.ref}^{{commit}}", capture=True).strip()
    stamp = run("git", "show", f"{commit}:src/version/version.go", capture=True)
    if f'const Value = "{args.version}"' not in stamp:
        raise ValueError("tagged source version does not match requested version")
    timestamp = epoch(commit)
    with tempfile.TemporaryDirectory(prefix="croc-source-") as temp:
        work = Path(temp)
        tree = work / f"croc-v{args.version}"
        tree.mkdir()
        archive = subprocess.check_output(["git", "archive", commit], cwd=ROOT)
        with tarfile.open(fileobj=io.BytesIO(archive)) as stream:
            stream.extractall(tree, filter="data")
        locks = {name: (tree / name).read_bytes() for name in ("go.mod", "go.sum")}
        run("go", "mod", "vendor", cwd=tree, env=dict(os.environ, GOTOOLCHAIN="local"))
        if any((tree / name).read_bytes() != data for name, data in locks.items()):
            raise ValueError("vendoring changed tagged module metadata")
        if args.verify:
            env = dict(os.environ, CGO_ENABLED="0", GOTOOLCHAIN="local", GOPROXY="off",
                       GOSUMDB="off", GOMODCACHE=str(work / "empty-modcache"))
            binary = work / "croc"
            run("go", "build", "-mod=vendor", "-buildvcs=false", "-trimpath",
                "-tags=netgo,osusergo", "-o", str(binary), ".", cwd=tree, env=env)
            if run(str(binary), "--version", capture=True).strip() != f"croc version {args.version}":
                raise ValueError("offline source binary reports wrong version")
        with out.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=timestamp) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as stream:
                for path in [tree, *sorted(tree.rglob("*"))]:
                    if ".git" in path.parts:
                        raise ValueError("source archive unexpectedly contains Git metadata")
                    entry = stream.gettarinfo(str(path), arcname=str(path.relative_to(work)))
                    entry.uid = entry.gid = 0
                    entry.uname = entry.gname = ""
                    entry.mtime = timestamp
                    entry.pax_headers = {}
                    entry.mode = 0o755 if entry.isdir() or entry.mode & 0o111 else 0o644
                    if entry.isfile():
                        with path.open("rb") as payload:
                            stream.addfile(entry, payload)
                    else:
                        stream.addfile(entry)
    print(f"Source archive: {out}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    for name in ("build", "packages", "source", "checksums", "verify-assets", "verify-draft"):
        command = commands.add_parser(name)
        command.add_argument("--version", type=version, required=True)
        if name in ("build", "source"):
            command.add_argument("--output", type=Path, required=True)
        if name in ("packages", "checksums", "verify-assets"):
            command.add_argument("--artifacts", type=Path, required=True)
        if name == "packages":
            command.add_argument("--output", type=Path)
            command.add_argument("--nfpm", default="nfpm")
            command.add_argument("--native-binary", type=Path)
        if name == "source":
            command.add_argument("--ref", required=True)
            command.add_argument("--verify", action="store_true")
        if name == "verify-draft":
            command.add_argument("--metadata", type=Path, required=True)
    args = parser.parse_args()
    if args.command == "build":
        build(args)
    elif args.command == "packages":
        make_packages(args)
    elif args.command == "source":
        source_archive(args)
    elif args.command == "verify-draft":
        check_draft(args.metadata, args.version)
    else:
        check_assets(args.artifacts, args.version, args.command == "checksums")


if __name__ == "__main__":
    main()
