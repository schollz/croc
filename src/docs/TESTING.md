# Testing

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
