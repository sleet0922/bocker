#!/bin/bash
# Bocker distro compatibility batch test.
LOG=/root/disto-test.log
rm -f "$LOG"
templates=$(bocker template list --json 2>/dev/null)
echo "templates: $(echo "$templates" | python3 -c 'import sys,json;print(len(json.load(sys.stdin)))')" | tee -a "$LOG"

echo "$templates" | python3 -c '
import sys,json
for t in json.load(sys.stdin):
    print(t["image"])
' > /root/disto-list.txt

i=0
while IFS= read -r img; do
  [ -n "$img" ] || continue
  name="t${i}"
  i=$((i+1))
  echo "=== [$i] $img (name=$name) ===" | tee -a "$LOG"
  out=$(timeout 900 bocker template install "$img" --name "$name" --network nat 2>&1)
  ec=$?
  status=$(curl -s --unix-socket /var/lib/bocker/incus/unix.socket "http://localhost/1.0/instances/$name/state" 2>/dev/null | python3 -c 'import sys,json
try: print(json.load(sys.stdin).get("metadata",{}).get("status","UNKNOWN"))
except Exception: print("UNKNOWN")' 2>/dev/null)
  reason=""
  if [ "$status" != "Running" ]; then
    reason="$(tail -2 /var/lib/bocker/incus/logs/$name/console.log 2>/dev/null)"
  fi
  echo "install_ec=$ec final_status=$status" | tee -a "$LOG"
  if [ -n "$reason" ]; then echo "reason: $reason" | tee -a "$LOG"; fi
  if [ "$ec" != "0" ] && [ "$status" != "Running" ]; then
    echo "RESULT: $img => FAIL ($status)" | tee -a "$LOG"
  else
    echo "RESULT: $img => OK ($status)" | tee -a "$LOG"
  fi
  bocker container remove "$name" >/dev/null 2>&1
  sleep 1
done < /root/disto-list.txt

echo "DONE" | tee -a "$LOG"