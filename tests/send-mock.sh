#!/usr/bin/env bash
# Sends mock data to StatMonitorEPD over serial and prints anything the device replies.
#   tests/send-mock.sh                          # /dev/ttyACM0, tests/mock-data.txt
#   tests/send-mock.sh COM10                    # Git Bash on Windows (COMn -> /dev/ttyS<n-1>)
#   tests/send-mock.sh /dev/ttyUSB0 other.txt
#   tests/send-mock.sh /dev/ttyACM0 - '2|b=85'  # send single lines instead of a file
# Close any serial monitor on the port first.
set -euo pipefail

port="${1:-/dev/ttyACM0}"
file="${2:-$(dirname "$0")/mock-data.txt}"
shift $(( $# > 2 ? 2 : $# ))
baud="${BAUD:-115200}"

if [[ "$port" =~ ^[Cc][Oo][Mm]([0-9]+)$ ]]; then
  port="/dev/ttyS$(( BASH_REMATCH[1] - 1 ))"
fi

if (( $# > 0 )); then
  lines=("$@")
else
  mapfile -t lines < <(grep -v -e '^#' -e '^[[:space:]]*$' "$file" | tr -d '\r')
fi

# raw mode, no echo; opening the port asserts DTR, which USB CDC needs before it delivers data
stty -F "$port" "$baud" raw -echo -hupcl
exec 3<>"$port"
for l in "${lines[@]}"; do
  printf '%s\n' "$l" >&3
  sleep 0.02
done

echo "Sent ${#lines[@]} line(s) to $port"
reply="$(timeout 0.5 cat <&3 || true)"
[[ -n "$reply" ]] && printf 'Device replied:\n%s\n' "$reply"
exec 3>&-
