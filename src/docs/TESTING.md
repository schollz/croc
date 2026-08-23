# Testing

## Transfer Update Checks

Send- and receive-side release checks are normally cached for 24 hours. Set
`CROC_DO_CHECK=1` to bypass a fresh cache, wait for a release request before the
transfer starts, and report whether a newer release is available:

```bash
CROC_DO_CHECK=1 croc send file.txt
```

The override waits up to two seconds for the request and reports when the current
version is already latest or when the check is unavailable. It does not display
a notice under `--quiet` or turn update-check failures into errors.

## Local Reconnect Interruptions

Use three terminals to run a local relay, receiver, and sender. From the repository root, build the binary and start a local relay in the first terminal:

```bash
go build -o ./bin/croc && ./bin/croc relay --ports 9009,9010
```

In the second terminal, start the receiver:

```bash
rm -rf croc-big.bin && ./bin/croc --debug --yes --overwrite --relay 127.0.0.1:9009 test-reconnect-code
```

In the third terminal, start the sender:

```bash
./bin/croc --relay 127.0.0.1:9009 --throttleUpload 512K --no-compress send --code test-reconnect-code --no-local --no-multi /tmp/croc-big.bin
```

While the transfer is running, interrupt one active socket.

For a data-channel drop:

```bash
sudo ss -K dst 127.0.0.1 dport = :9010
```

For a control-channel drop:

```bash
sudo ss -K dst 127.0.0.1 dport = :9009
```

If no connection is killed, inspect the active croc sockets and adjust the port:

```bash
sudo ss -tnp | grep croc
```

## Experimental Direct UDP/QUIC

Build croc and start the same local relay shown above. Opt in on both peers and
disable public STUN for a deterministic LAN check:

```bash
./bin/croc --debug --yes --overwrite --relay 127.0.0.1:9009 \
  --experimental-direct-udp --experimental-stun-server off test-direct-code

./bin/croc --debug --relay 127.0.0.1:9009 \
  --experimental-direct-udp --experimental-stun-server off \
  --no-compress send --code test-direct-code /tmp/croc-big.bin
```

Both terminals must report `Using experimental direct UDP (QUIC)`. If only one
peer has the flag, they report the mismatch and transfer over TCP. If both peers
advertise the feature but probing or the QUIC handshake fails, the transfer must
fail without sending payload over the TCP data channels.

The transport-level loopback benchmark is available with:

```bash
go test ./src/directquic -run '^$' -bench BenchmarkDirectQUICStream -benchmem
```

For comparable end-to-end results, use a 1 GiB incompressible file and
`--no-compress`, run five trials each through the normal LAN TCP path, a relay
TCP path, and direct QUIC, and report the median and range. On Linux, repeat with
`tc netem` at 50 ms RTT and 0%, 0.5%, and 1% loss. Record sender and receiver
CPU alongside goodput; `--debug` prints QUIC RTT, byte, packet-loss, and GSO
statistics when the connection closes.
