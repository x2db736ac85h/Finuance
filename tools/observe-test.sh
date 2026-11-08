#!/usr/bin/env bash
# Self-test of tools/observe.sh (make check) with a fake kubectl, so no cluster is needed: a free
# port is forwarded on the loopback addresses only and forwarded again after the forward stops, a port
# in use or an unreachable target is refused before anything starts, an unknown tool is a usage error,
# and stopping it leaves no forward behind.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

work=$(mktemp -d)
listener=""
cleanup() {
  [ -z "$listener" ] || kill "$listener" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT
fail() {
  echo "observe-test: $*" >&2
  exit 1
}

# the fake: `get` answers unless FAKE_UNREACHABLE is set; `port-forward` records its arguments and
# stops after a moment, like a forward whose pod restarted
mkdir "$work/bin"
cat >"$work/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
case " $* " in
  *" get "*) [ -z "${FAKE_UNREACHABLE:-}" ] ;;
  *" port-forward "*) echo "$*" >>"$FAKE_LOG"; sleep 0.3; exit 1 ;;
esac
EOF
chmod +x "$work/bin/kubectl"
export PATH="$work/bin:$PATH" FAKE_LOG="$work/forwards.log" OBSERVE_RETRY=0.1

# lines in the fake's log (0 before the first forward)
forwards() { if [ -f "$FAKE_LOG" ]; then wc -l <"$FAKE_LOG"; else echo 0; fi; }

# a port nothing listens on
free_port() {
  local p
  for p in $(seq 41000 41100); do
    if ! (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then echo "$p"; return; fi
  done
  fail "no free port between 41000 and 41100"
}
port=$(free_port)
table="tool|ns|svc/thing|$port|80|http://localhost:$port|no sign-in"

# 1. forwarded on 127.0.0.1 and [::1] only, and again after it stopped; stopping it ends everything
OBSERVE_TABLE=$table tools/observe.sh >"$work/out" 2>&1 &
pid=$!
for _ in $(seq 50); do
  [ "$(forwards)" -ge 2 ] && break
  sleep 0.1
done
if [ "$(forwards)" -lt 2 ]; then
  kill "$pid" 2>/dev/null || true
  fail "the forward wasn't started again after it stopped: $(cat "$work/out")"
fi
grep -qx -- "-n ns port-forward --address 127.0.0.1,::1 svc/thing $port:80" "$FAKE_LOG" ||
  fail "unexpected kubectl arguments: $(head -1 "$FAKE_LOG")"
grep -q "http://localhost:$port" "$work/out" || fail "the URL wasn't printed: $(cat "$work/out")"
# TERM, not INT: a background job of a script starts with SIGINT ignored, so it can't be trapped
# there (Ctrl-C in a terminal reaches the foreground script, which traps INT the same way)
kill -TERM "$pid"
wait "$pid" || true
sleep 0.5
if pgrep -f "kubectl -n ns port-forward" >/dev/null; then fail "a forward is still running after Ctrl-C"; fi

# 2. a port in use is refused before anything starts
python3 -c 'import socket, sys, time
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", int(sys.argv[1]))); s.listen(); time.sleep(30)' "$port" &
listener=$!
for _ in $(seq 50); do (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null && break; sleep 0.1; done
: >"$FAKE_LOG"
if OBSERVE_TABLE=$table tools/observe.sh >"$work/out" 2>&1; then fail "a port in use wasn't refused"; fi
grep -q "port $port (tool) is already in use" "$work/out" || fail "unexpected message: $(cat "$work/out")"
[ ! -s "$FAKE_LOG" ] || fail "a forward started although the port was in use"
kill "$listener"
listener=""

# 2b. a port held on [::1] only (what a half-started forward leaves) is refused too
if python3 -c 'import socket; socket.socket(socket.AF_INET6).bind(("::1", 0))' 2>/dev/null; then
  python3 -c 'import socket, sys, time
s = socket.socket(socket.AF_INET6); s.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
s.bind(("::1", int(sys.argv[1]))); s.listen(); time.sleep(30)' "$port" &
  listener=$!
  for _ in $(seq 50); do (exec 3<>"/dev/tcp/::1/$port") 2>/dev/null && break; sleep 0.1; done
  if OBSERVE_TABLE=$table tools/observe.sh >"$work/out" 2>&1; then fail "a port held on [::1] only wasn't refused"; fi
  kill "$listener"
  listener=""
else
  echo "observe-test: no IPv6 loopback here; the [::1] case is skipped"
fi

# 3. an unreachable target is refused; 4. an unknown tool is a usage error
if FAKE_UNREACHABLE=1 OBSERVE_TABLE=$table tools/observe.sh >"$work/out" 2>&1; then fail "an unreachable target wasn't refused"; fi
grep -q "ns/svc/thing isn't reachable" "$work/out" || fail "unexpected message: $(cat "$work/out")"
status=0
OBSERVE_TABLE=$table tools/observe.sh nosuchtool >"$work/out" 2>&1 || status=$?
[ "$status" -eq 2 ] || fail "an unknown tool gave exit status $status, not 2"

echo "observe-test: ok"
