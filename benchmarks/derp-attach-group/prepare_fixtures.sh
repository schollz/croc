#!/usr/bin/env bash

set -euo pipefail

remote="${BENCH_REMOTE:-root@134.122.43.205}"
local_root="${BENCH_LOCAL_ROOT:-/tmp/croc-attach-bench}"
remote_root="${BENCH_REMOTE_ROOT:-/tmp/croc-attach-bench}"
fixture_mib="${BENCH_FIXTURE_MIB:-256}"
suffix="${BENCH_FIXTURE_SUFFIX:-${fixture_mib}m}"
local_fixture="${local_root}/data/fixture-local-${suffix}.bin"
remote_fixture="${remote_root}/data/fixture-remote-${suffix}.bin"

mkdir -p "${local_root}/data" "${local_root}/out"
if [ ! -f "${local_fixture}" ] || [ "$(stat -f %z "${local_fixture}")" -ne "$((fixture_mib * 1024 * 1024))" ]; then
	dd if=/dev/urandom of="${local_fixture}" bs=1048576 count="${fixture_mib}" status=progress
fi
ssh "${remote}" "mkdir -p '${remote_root}/data' '${remote_root}/out'; if [ ! -f '${remote_fixture}' ] || [ \"\$(stat -c %s '${remote_fixture}' 2>/dev/null || printf 0)\" -ne '$((fixture_mib * 1024 * 1024))' ]; then dd if=/dev/urandom of='${remote_fixture}' bs=1048576 count='${fixture_mib}' status=progress; fi"

shasum -a 256 "${local_fixture}"
ssh "${remote}" "sha256sum '${remote_fixture}'"
