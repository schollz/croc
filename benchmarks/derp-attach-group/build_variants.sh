#!/usr/bin/env bash

set -euo pipefail

remote="${BENCH_REMOTE:-root@134.122.43.205}"
local_root="${BENCH_LOCAL_ROOT:-/tmp/croc-attach-bench}"
remote_root="${BENCH_REMOTE_ROOT:-/tmp/croc-attach-bench}"
configs="${BENCH_CONFIGS:-8:4}"
upload="${BENCH_UPLOAD:-0}"
build_force_relay="${BENCH_BUILD_FORCE_RELAY:-1}"
package_path="github.com/schollz/croc/v11/src/croc"

mkdir -p "${local_root}"

build_one() {
	goos="$1"
	goarch="$2"
	output="$3"
	shift 3
	CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" go build -trimpath "$@" -o "${output}" .
}

build_one darwin arm64 "${local_root}/croc-legacy-darwin-arm64"
build_one linux amd64 "${local_root}/croc-legacy-linux-amd64"

for config in ${configs}; do
	streams="${config%%:*}"
	raw_paths="${config##*:}"
	name="group-s${streams}-p${raw_paths}"
	ldflags="-X ${package_path}.derpAttachGroupBuildEnable=true -X ${package_path}.derpAttachGroupBuildStreams=${streams} -X ${package_path}.derpAttachGroupBuildRawPaths=${raw_paths} -X ${package_path}.derpAttachGroupBuildBudgetMS=3000 -X ${package_path}.derpAttachGroupBuildRelay=false"
	build_one darwin arm64 "${local_root}/croc-${name}-darwin-arm64" -tags croc_attach_bench -ldflags "${ldflags}"
	build_one linux amd64 "${local_root}/croc-${name}-linux-amd64" -tags croc_attach_bench -ldflags "${ldflags}"
done

if [ "${build_force_relay}" = 1 ]; then
	ldflags="-X ${package_path}.derpAttachGroupBuildEnable=true -X ${package_path}.derpAttachGroupBuildStreams=8 -X ${package_path}.derpAttachGroupBuildRawPaths=4 -X ${package_path}.derpAttachGroupBuildBudgetMS=3000 -X ${package_path}.derpAttachGroupBuildRelay=true"
	build_one darwin arm64 "${local_root}/croc-group-force-relay-darwin-arm64" -tags croc_attach_bench -ldflags "${ldflags}"
	build_one linux amd64 "${local_root}/croc-group-force-relay-linux-amd64" -tags croc_attach_bench -ldflags "${ldflags}"
fi

(
	cd "${local_root}"
	shasum -a 256 croc-*-darwin-arm64 croc-*-linux-amd64 >binary-manifest.sha256
)

if [ "${upload}" = 1 ]; then
	ssh "${remote}" "mkdir -p '${remote_root}'"
	scp "${local_root}"/croc-*-linux-amd64 "${remote}:${remote_root}/"
	scp "${local_root}/binary-manifest.sha256" "${remote}:${remote_root}/"
fi
