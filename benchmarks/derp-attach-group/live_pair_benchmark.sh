#!/usr/bin/env bash

set -euo pipefail

remote="${BENCH_REMOTE:-root@134.122.43.205}"
remote_host="${BENCH_REMOTE_HOST:-134.122.43.205}"
pairs="${BENCH_PAIRS:-3}"
start_pair="${BENCH_START_PAIR:-1}"
iperf_seconds="${BENCH_IPERF_SECONDS:-10}"
directions="${BENCH_DIRECTIONS:-l2r r2l}"
append_results="${BENCH_APPEND:-0}"
skip_completed="${BENCH_SKIP_COMPLETED:-0}"
candidate_streams="${BENCH_STREAMS:-8}"
candidate_raw_paths="${BENCH_RAW_PATHS:-4}"
candidate="${BENCH_CANDIDATE:-group-s${candidate_streams}-p${candidate_raw_paths}}"
expected_mode="${BENCH_EXPECT_MODE:-raw-direct}"
expected_path="${BENCH_EXPECT_PATH:-direct}"
expected_streams="${BENCH_EXPECT_STREAMS:-${candidate_streams}}"
run_id="${BENCH_RUN_ID:-$(date +%s)}"
local_root="${BENCH_LOCAL_ROOT:-/tmp/croc-attach-bench}"
remote_root="${BENCH_REMOTE_ROOT:-/tmp/croc-attach-bench}"
result_dir="${BENCH_RESULT_DIR:?set BENCH_RESULT_DIR}"

local_source="${BENCH_LOCAL_SOURCE:-${local_root}/data/fixture-local-1g.bin}"
remote_source="${BENCH_REMOTE_SOURCE:-${remote_root}/data/fixture-remote-1g.bin}"
local_output="${BENCH_LOCAL_OUTPUT:-${local_root}/out/$(basename "${remote_source}")}"
remote_output="${BENCH_REMOTE_OUTPUT:-${remote_root}/out/$(basename "${local_source}")}"
bytes="$(stat -f %z "${local_source}")"
local_sha="$(shasum -a 256 "${local_source}" | awk '{print $1}')"
remote_sha="$(ssh "${remote}" "sha256sum '${remote_source}'" | awk '{print $1}')"

mkdir -p "${result_dir}/logs" "${result_dir}/iperf"
csv="${result_dir}/results.csv"
if [ "${append_results}" != 1 ] || [ ! -f "${csv}" ]; then
	printf '%s\n' 'direction,pair,sequence,variant,wall_seconds,bytes,iperf_bps,normalized_goodput,sha_ok,mode_proof,local_exit,remote_exit' >"${csv}"
fi

cat >"${result_dir}/environment.txt" <<EOF
remote=${remote}
pairs=${pairs}
iperf_seconds=${iperf_seconds}
candidate=${candidate}
candidate_streams=${candidate_streams}
candidate_raw_paths=${candidate_raw_paths}
expected_mode=${expected_mode}
expected_path=${expected_path}
expected_streams=${expected_streams}
bytes=${bytes}
local_sha256=${local_sha}
remote_sha256=${remote_sha}
local_uname=$(uname -a)
local_go=$(go version)
EOF
ssh "${remote}" 'uname -a; iperf3 --version | head -1' >>"${result_dir}/environment.txt"
shasum -a 256 "${local_root}"/croc-*-darwin-arm64 >>"${result_dir}/environment.txt"
ssh "${remote}" "sha256sum '${remote_root}'/croc-*-linux-amd64" >>"${result_dir}/environment.txt"

now_ns() {
	python3 -c 'import time; print(time.time_ns())'
}

run_iperf() {
	direction="$1"
	label="$2"
	server_log="${result_dir}/iperf/${label}-server.json"
	client_log="${result_dir}/iperf/${label}-client.json"
	attempt=1
	while [ "${attempt}" -le 3 ]; do
		ssh "${remote}" 'iperf3 -s -1 -J' >"${server_log}" 2>&1 &
		server_pid=$!
		sleep 3
		set +e
		if [ "${direction}" = "r2l" ]; then
			iperf3 -c "${remote_host}" -R -t "${iperf_seconds}" -J >"${client_log}"
		else
			iperf3 -c "${remote_host}" -t "${iperf_seconds}" -J >"${client_log}"
		fi
		client_exit=$?
		set -e
		if [ "${client_exit}" -eq 0 ] && jq -e '(.error // "") == "" and (.end.sum_received.bits_per_second // 0) > 0' "${client_log}" >/dev/null; then
			wait "${server_pid}" || true
			jq -r '.end.sum_received.bits_per_second' "${client_log}"
			return 0
		fi
		kill "${server_pid}" 2>/dev/null || true
		wait "${server_pid}" 2>/dev/null || true
		ssh "${remote}" "pkill -f '^iperf3 -s -1 -J$' || true"
		attempt=$((attempt + 1))
	done
	printf '%s\n' "iperf control failed after three attempts for ${label}" >&2
	return 1
}

local_binary() {
	case "$1" in
	legacy) printf '%s/croc-legacy-darwin-arm64' "${local_root}" ;;
	"${candidate}") printf '%s/croc-%s-darwin-arm64' "${local_root}" "${candidate}" ;;
	*) return 1 ;;
	esac
}

remote_binary() {
	case "$1" in
	legacy) printf '%s/croc-legacy-linux-amd64' "${remote_root}" ;;
	"${candidate}") printf '%s/croc-%s-linux-amd64' "${remote_root}" "${candidate}" ;;
	*) return 1 ;;
	esac
}

append_result() {
	direction="$1"
	pair="$2"
	sequence="$3"
	variant="$4"
	wall="$5"
	capacity="$6"
	sha_ok="$7"
	mode_proof="$8"
	local_exit="$9"
	remote_exit="${10}"
	normalized="$(python3 - "${bytes}" "${wall}" "${capacity}" <<'PY'
import sys
size, wall, capacity = map(float, sys.argv[1:])
print((size * 8.0 / wall) / capacity if wall > 0 and capacity > 0 else 0)
PY
)"
	printf '%s,%s,%s,%s,%.6f,%s,%.3f,%.9f,%s,%s,%s,%s\n' \
		"${direction}" "${pair}" "${sequence}" "${variant}" "${wall}" "${bytes}" \
		"${capacity}" "${normalized}" "${sha_ok}" "${mode_proof}" "${local_exit}" "${remote_exit}" >>"${csv}"
}

candidate_mode_proof() {
	local local_log="$1"
	local remote_log="$2"
	local proof
	proof="$( (grep -a -h "DERP transport summary: mode=${expected_mode} path=${expected_path} streams=${expected_streams} raw_paths=" "${local_log}" "${remote_log}" || true) | wc -l | tr -d ' ')"
	if [ "${proof}" -eq 2 ]; then
		printf '%s-%sof2' "${expected_mode}" "${proof}"
	else
		printf '%s-unproven-%sof2' "${expected_mode}" "${proof}"
	fi
}

run_l2r() {
	pair="$1"
	sequence="$2"
	variant="$3"
	label="l2r-p${pair}-s${sequence}-${variant}"
	code="ag${run_id}${pair}${sequence}${variant}l2r"
	local_log="${result_dir}/logs/${label}-local-sender.log"
	remote_log="${result_dir}/logs/${label}-remote-receiver.log"
	capacity="$(run_iperf l2r "${label}")"
	ssh "${remote}" "rm -f '${remote_output}'"
	start="$(now_ns)"
	set +e
	/usr/bin/time -l "$(local_binary "${variant}")" --debug --yes --no-compress \
		--disable-clipboard --ignore-stdin --transport derp send --code "${code}" \
		--no-local "${local_source}" >"${local_log}" 2>&1 &
	local_pid=$!
	sleep 1
	ssh "${remote}" "/usr/bin/time -v '$(remote_binary "${variant}")' --debug --yes --overwrite --no-compress --disable-clipboard --ignore-stdin --transport derp --out '${remote_root}/out' '${code}'" >"${remote_log}" 2>&1
	remote_exit=$?
	wait "${local_pid}"
	local_exit=$?
	set -e
	end="$(now_ns)"
	wall="$(python3 - "${start}" "${end}" <<'PY'
import sys
print((int(sys.argv[2]) - int(sys.argv[1])) / 1e9)
PY
)"
	got_sha="$( (ssh "${remote}" "test -f '${remote_output}' && sha256sum '${remote_output}'" 2>/dev/null || true) | awk '{print $1}')"
	sha_ok=false
	[ "${got_sha}" = "${local_sha}" ] && sha_ok=true
	if [ "${variant}" = "${candidate}" ]; then
		mode_proof="$(candidate_mode_proof "${local_log}" "${remote_log}")"
	else
		proof="$( (grep -a -h 'DERP transport summary: mode=legacy .*streams=1' "${local_log}" "${remote_log}" || true) | wc -l | tr -d ' ')"
		mode_proof="legacy-${proof}of2"
	fi
	append_result l2r "${pair}" "${sequence}" "${variant}" "${wall}" "${capacity}" "${sha_ok}" "${mode_proof}" "${local_exit}" "${remote_exit}"
	printf '%s %s %s wall=%ss capacity=%sbps sha=%s proof=%s\n' l2r "${pair}" "${variant}" "${wall}" "${capacity}" "${sha_ok}" "${mode_proof}"
}

run_r2l() {
	pair="$1"
	sequence="$2"
	variant="$3"
	label="r2l-p${pair}-s${sequence}-${variant}"
	code="ag${run_id}${pair}${sequence}${variant}r2l"
	remote_log="${result_dir}/logs/${label}-remote-sender.log"
	local_log="${result_dir}/logs/${label}-local-receiver.log"
	capacity="$(run_iperf r2l "${label}")"
	rm -f "${local_output}"
	start="$(now_ns)"
	set +e
	ssh "${remote}" "/usr/bin/time -v '$(remote_binary "${variant}")' --debug --yes --no-compress --disable-clipboard --ignore-stdin --transport derp send --code '${code}' --no-local '${remote_source}'" >"${remote_log}" 2>&1 &
	remote_pid=$!
	wait_count=0
	while ! grep -a -q 'connection established' "${remote_log}" 2>/dev/null; do
		if ! kill -0 "${remote_pid}" 2>/dev/null || [ "${wait_count}" -ge 100 ]; then
			break
		fi
		sleep 0.2
		wait_count=$((wait_count + 1))
	done
	sleep 0.5
	/usr/bin/time -l "$(local_binary "${variant}")" --debug --yes --overwrite --no-compress \
		--disable-clipboard --ignore-stdin --transport derp --out "${local_root}/out" "${code}" >"${local_log}" 2>&1
	local_exit=$?
	wait "${remote_pid}"
	remote_exit=$?
	set -e
	end="$(now_ns)"
	wall="$(python3 - "${start}" "${end}" <<'PY'
import sys
print((int(sys.argv[2]) - int(sys.argv[1])) / 1e9)
PY
)"
	got_sha="$( (test -f "${local_output}" && shasum -a 256 "${local_output}" || true) | awk '{print $1}')"
	sha_ok=false
	[ "${got_sha}" = "${remote_sha}" ] && sha_ok=true
	if [ "${variant}" = "${candidate}" ]; then
		mode_proof="$(candidate_mode_proof "${local_log}" "${remote_log}")"
	else
		proof="$( (grep -a -h 'DERP transport summary: mode=legacy .*streams=1' "${local_log}" "${remote_log}" || true) | wc -l | tr -d ' ')"
		mode_proof="legacy-${proof}of2"
	fi
	append_result r2l "${pair}" "${sequence}" "${variant}" "${wall}" "${capacity}" "${sha_ok}" "${mode_proof}" "${local_exit}" "${remote_exit}"
	printf '%s %s %s wall=%ss capacity=%sbps sha=%s proof=%s\n' r2l "${pair}" "${variant}" "${wall}" "${capacity}" "${sha_ok}" "${mode_proof}"
}

run_direction() {
	direction="$1"
	pair="${start_pair}"
	while [ "${pair}" -le "${pairs}" ]; do
		if [ $((pair % 2)) -eq 1 ]; then
			first=legacy
			second="${candidate}"
		else
			first="${candidate}"
			second=legacy
		fi
		if [ "${skip_completed}" != 1 ] || ! grep -q "^${direction},${pair},[^,]*,${first}," "${csv}"; then
			"run_${direction}" "${pair}" 1 "${first}"
		fi
		if [ "${skip_completed}" != 1 ] || ! grep -q "^${direction},${pair},[^,]*,${second}," "${csv}"; then
			"run_${direction}" "${pair}" 2 "${second}"
		fi
		pair=$((pair + 1))
	done
}

for direction in ${directions}; do
	run_direction "${direction}"
done
