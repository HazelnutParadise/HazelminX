#!/bin/sh
# Start a binary as an unprivileged user next to 300 idle processes, wait for
# it to settle, then run measure-idle.sh on it. Meant to run inside a Linux
# container with this directory and the binary mounted at /m:
#
#   docker run --rm --pid=host --cpuset-cpus=0,1 -v "$PWD":/m:ro \
#     buildpack-deps:trixie sh /m/measure-in-container.sh /m/hazelminx-agent \
#     --config /tmp/hx/absent.toml --state-dir /tmp/hx/state --log-format json run
#
# A container normally sees only its own few processes, which would make a
# process-table scan look cheaper than on a real server; the idle sleeps make
# up for that. Environment overrides: MEASURE_WINDOW (seconds, default 300),
# MEASURE_SETTLE (seconds before the window, default 30), MEASURE_SLEEPS
# (idle processes to start, default 300). Needs cgroup v2 for the cpuset; on
# other hosts it records the aggregate cpu line instead.
set -eu
[ $# -ge 1 ] || { echo "usage: measure-in-container.sh <binary> [args...]" >&2; exit 2; }
here=$(dirname "$0")
window=${MEASURE_WINDOW:-300}
settle=${MEASURE_SETTLE:-30}
sleeps=${MEASURE_SLEEPS:-300}
for n in "$window" "$settle"; do
	case $n in '' | *[!0-9]* | 0) echo "MEASURE_WINDOW and MEASURE_SETTLE must be positive integers" >&2; exit 2 ;; esac
done
case $sleeps in '' | *[!0-9]*) echo "MEASURE_SLEEPS must be a non-negative integer" >&2; exit 2 ;; esac

mkdir -p /tmp/hx && chown 65534:65534 /tmp/hx
i=0
while [ $i -lt "$sleeps" ]; do sleep 100000 & i=$((i + 1)); done

setpriv --reuid=65534 --regid=65534 --clear-groups "$@" >/tmp/hx/out.log 2>&1 &
pid=$!
sleep 1
echo "pid=$pid uid=$(awk '/^Uid:/ { print $2 }' "/proc/$pid/status")"
[ "$settle" -gt 1 ] && sleep $((settle - 1))
# CPU lines of the cores this container may use (field 9 is steal), taken
# right before and after the window.
# cpuset.cpus.effective looks like "0-1" or "0,2-3"; without it use the
# aggregate "cpu" line (empty suffix).
if [ -r /sys/fs/cgroup/cpuset.cpus.effective ]; then
	cpus=$(tr ',' '\n' </sys/fs/cgroup/cpuset.cpus.effective | awk -F- '{ for (c = $1; c <= ($2 == "" ? $1 : $2); c++) print c }')
else
	cpus=""
fi
cpu_lines() {
	echo "loadavg=$(cut -d' ' -f1-3 /proc/loadavg)"
	if [ -n "$cpus" ]; then
		for c in $cpus; do grep "^cpu$c " /proc/stat || true; done
	else
		grep '^cpu ' /proc/stat
	fi
}
cpu_lines
rc=0
"$here/measure-idle.sh" "$pid" "$window" 10 || rc=$?
cpu_lines
echo "-- process log"
cat /tmp/hx/out.log
if [ "$rc" -eq 0 ]; then
	kill -TERM "$pid"
	wait "$pid" || rc=$?
	echo "exit_status=$rc"
fi
exit "$rc"
