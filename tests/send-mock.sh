#!/usr/bin/env bash
# Sends mock data to StatMonitorEPD over serial and prints anything the device replies.
#   tests/send-mock.sh                              # /dev/ttyACM0, all of tests/mock-data.txt
#   tests/send-mock.sh -p COM10                     # Git Bash on Windows (COMn -> /dev/ttyS<n-1>)
#   tests/send-mock.sh -p /dev/ttyUSB0 -f other.txt
#   tests/send-mock.sh -p COM10 '2|b=85' '4|on=1'   # send literal lines
#   tests/send-mock.sh -t bar -i 2                  # one field of one list item, random value
#   tests/send-mock.sh -t disk -i H                 # header works too
#   tests/send-mock.sh -t bar -n 20 -d 100          # rapid updates, should coalesce
#
# Options: -p port, -f file, -t test, -i item (1..12 or H), -n count, -d interval ms, -b baud
# -t modes (random values each time, so every send is a real change):
#   mock     the mock file (default)
#   item     whole list item: name, ip, disk, ram, bar, online
#   name, ip, disk, ram, bar, online     only that field (an update within the item)
#   fields   name, ip, disk, ram, bar one after another, -d ms apart
# Close any serial monitor on the port first.
set -euo pipefail

port=/dev/ttyACM0
file="$(dirname "$0")/mock-data.txt"
test=mock
item=1
count=1
interval=0
baud=115200
while getopts "p:f:t:i:n:d:b:" opt; do
  case "$opt" in
    p) port="$OPTARG" ;;
    f) file="$OPTARG" ;;
    t) test="$OPTARG" ;;
    i) item="$OPTARG" ;;
    n) count="$OPTARG" ;;
    d) interval="$OPTARG" ;;
    b) baud="$OPTARG" ;;
    *) sed -n '2,17p' "$0"; exit 1 ;;
  esac
done
shift $((OPTIND - 1))

case "$test" in
  mock|item|name|ip|disk|ram|bar|online|fields) ;;
  *) echo "unknown test '$test'" >&2; exit 1 ;;
esac

if [[ "$port" =~ ^[Cc][Oo][Mm]([0-9]+)$ ]]; then
  port="/dev/ttyS$(( BASH_REMATCH[1] - 1 ))"
fi

pick() { local a=("$@"); echo "${a[RANDOM % ${#a[@]}]}"; }

field() {
  local t
  case "$1" in
    name)   echo "n=Container $item-$(( RANDOM % 100 ))" ;;
    ip)     echo "ip=192.168.1.$(( RANDOM % 253 + 2 ))" ;;
    disk)   t=$(pick 32 64 128 256 512); echo "d=$(( RANDOM % t + 1 ))/$t" ;;
    ram)    t=$(pick 4 8 16 32); echo "r=$(( RANDOM % (t + 1) ))/$t" ;;
    bar)    echo "b=$(( RANDOM % 101 ))" ;;
    online) echo "on=$(( RANDOM % 2 ))" ;;
  esac
}

test_lines() {
  case "$test" in
    mock)   grep -v -e '^#' -e '^[[:space:]]*$' "$file" | tr -d '\r' ;;
    item)   local l="$item"
            for f in name ip disk ram bar online; do l+="|$(field "$f")"; done
            echo "$l" ;;
    fields) for f in name ip disk ram bar; do echo "$item|$(field "$f")"; done ;;
    *)      echo "$item|$(field "$test")" ;;
  esac
}

# raw mode, no echo; opening the port asserts DTR, which USB CDC needs before it delivers data
stty -F "$port" "$baud" raw -echo -hupcl
exec 3<>"$port"

delay=$(awk -v ms="$interval" 'BEGIN { printf "%.3f", (ms < 20 ? 20 : ms) / 1000 }')
sent=0
for (( i = 0; i < count; i++ )); do
  if (( $# > 0 )); then lines=("$@"); else mapfile -t lines < <(test_lines); fi
  for l in "${lines[@]}"; do
    printf '%s\n' "$l" >&3
    echo "> $l"
    sent=$((sent + 1))
    sleep "$delay"
  done
done

reply="$(timeout 0.5 cat <&3 || true)"
[[ -n "$reply" ]] && printf '%s\n' "$reply"
echo "Sent $sent line(s) to $port"
exec 3>&-
