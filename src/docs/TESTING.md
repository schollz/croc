# Testing

## Local Reconnect Interruptions

When testing reconnect behavior against a local relay, interrupt one active socket while the transfer is running.

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
