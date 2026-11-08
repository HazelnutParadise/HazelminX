#!/bin/sh
# Measure idle CPU and resident memory of a running process (spec 38.1).
#
# Usage: measure-idle.sh <pid> [window_seconds] [sample_interval_seconds]
#
# CPU: (utime + stime) delta of the process over the window, from
# /proc/<pid>/stat, divided by the elapsed seconds. 100% means one full core.
# Threads are included; child processes are not (spec: "不含子程序").
# Memory: VmRSS sampled every interval, plus the kernel's VmHWM peak; the
# verdict uses the larger of the two.
set -eu

usage="usage: measure-idle.sh <pid> [window_seconds] [sample_interval_seconds]"
pid=${1:?$usage}
window=${2:-300}
interval=${3:-10}
for n in "$pid" "$window" "$interval"; do
	case $n in '' | *[!0-9]* | 0) echo "$usage (all values must be positive integers)" >&2; exit 2 ;; esac
done
[ "$interval" -le "$window" ] || { echo "sample interval must not exceed the window" >&2; exit 2; }
[ -r "/proc/$pid/stat" ] || { echo "no such process: $pid" >&2; exit 1; }
hz=$(getconf CLK_TCK)

# Fields after the parenthesised comm: state is field 3, utime 14, stime 15,
# starttime 22 (numbered as in proc(5)).
stat_field() { sed 's/^.*) //' "/proc/$pid/stat" | awk -v f="$1" '{ print $(f - 2) }'; }
cpu_ticks() { sed 's/^.*) //' "/proc/$pid/stat" | awk '{ print $12 + $13 }'; }
status_kib() { awk -v k="$1:" '$1 == k { print $2 }' "/proc/$pid/status"; }
alive() {
	[ -r "/proc/$pid/stat" ] && [ "$(stat_field 22)" = "$start_id" ] && [ "$(stat_field 3)" != Z ]
}

start_id=$(stat_field 22)
start_ticks=$(cpu_ticks)
start_time=$(date +%s)
samples=0
rss_sum=0
rss_max=0
elapsed=0
while [ "$elapsed" -lt "$window" ]; do
	sleep "$interval"
	alive || { echo "process $pid exited during measurement" >&2; exit 1; }
	rss=$(status_kib VmRSS)
	samples=$((samples + 1))
	rss_sum=$((rss_sum + rss))
	[ "$rss" -gt "$rss_max" ] && rss_max=$rss
	elapsed=$(($(date +%s) - start_time))
done
end_ticks=$(cpu_ticks)
hwm=$(status_kib VmHWM)
threads=$(status_kib Threads)
procs=$(ls -d /proc/[0-9]* | wc -l)

echo "kernel=$(uname -r) arch=$(uname -m) nproc=$(nproc) visible_processes=$procs"
awk -v t0="$start_ticks" -v t1="$end_ticks" -v hz="$hz" -v secs="$elapsed" \
	-v n="$samples" -v sum="$rss_sum" -v max="$rss_max" -v hwm="$hwm" -v th="$threads" 'BEGIN {
	cpu = (t1 - t0) / hz / secs * 100
	peak = (hwm > max ? hwm : max) / 1024
	printf "window_seconds=%d samples=%d threads=%d\n", secs, n, th
	printf "cpu_ticks=%d clk_tck=%d cpu_percent_of_one_core=%.4f\n", t1 - t0, hz, cpu
	printf "rss_avg_mib=%.2f rss_max_mib=%.2f vm_hwm_mib=%.2f peak_mib=%.2f\n", sum / n / 1024, max / 1024, hwm / 1024, peak
	ok = (cpu < 2 && peak < 200) ? "PASS" : "FAIL"
	printf "target cpu<2%% peak<200MiB: %s\n", ok
}'
