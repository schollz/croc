# Plan: Replace panic with errgroup.Group in processMessagePake

## Problem

In [`src/croc/croc.go`](src/croc/croc.go:1526) at line 1526, `panic(err)` is called inside an anonymous goroutine launched via `go func(j int)`. There is no `recover` in this goroutine, and the `recover` in the parent function [`processMessagePake`](src/croc/croc.go:1442) cannot catch a panic from a child goroutine. The result — the entire process crashes when connecting to a relay port fails.

## Solution

Replace `sync.WaitGroup` + `panic` with `errgroup.Group`, which:
- Catches the error from any goroutine
- Returns it via `g.Wait()`
- Allows the caller to handle the error gracefully

## Changes

### 1. Add dependency `golang.org/x/sync`

Run:
```bash
go get golang.org/x/sync
go mod tidy
```

### 2. Add import in [`src/croc/croc.go`](src/croc/croc.go:3)

Add to the `import` block:
```go
"golang.org/x/sync/errgroup"
```

### 3. Replace code in [`processMessagePake`](src/croc/croc.go:1501) (lines 1501–1543)

**Before:**
```go
// connects to the other ports of the server for transfer
var wg sync.WaitGroup
wg.Add(len(c.Options.RelayPorts))
for i := 0; i < len(c.Options.RelayPorts); i++ {
    log.Debugf("port: [%s]", c.Options.RelayPorts[i])
    go func(j int) {
        defer wg.Done()
        var host string
        if c.Options.RelayAddress == "127.0.0.1" {
            host = c.Options.RelayAddress
        } else {
            host, _, err = net.SplitHostPort(c.Options.RelayAddress)
            if err != nil {
                log.Errorf("bad relay address %s", c.Options.RelayAddress)
                return
            }
        }
        server := net.JoinHostPort(host, c.Options.RelayPorts[j])
        log.Debugf("connecting to %s", server)
        c.conn[j+1], _, _, err = tcp.ConnectToTCPServer(
            server,
            c.Options.RelayPassword,
            fmt.Sprintf("%s-%d", c.Options.RoomName, j),
        )
        if err != nil {
            panic(err)
        }
        log.Debugf("connected to %s", server)
        if !c.Options.IsSender {
            go c.receiveData(j)
        }
    }(i)
}
wg.Wait()
if !c.Options.IsSender {
    log.Debug("sending external IP")
    err = message.Send(c.conn[0], c.Key, message.Message{
        Type:    message.TypeExternalIP,
        Message: c.ExternalIP,
        Bytes:   m.Bytes,
    })
}
return
```

**After:**
```go
// connects to the other ports of the server for transfer
var g errgroup.Group
for i := 0; i < len(c.Options.RelayPorts); i++ {
    log.Debugf("port: [%s]", c.Options.RelayPorts[i])
    j := i
    g.Go(func() error {
        var host string
        if c.Options.RelayAddress == "127.0.0.1" {
            host = c.Options.RelayAddress
        } else {
            var splitErr error
            host, _, splitErr = net.SplitHostPort(c.Options.RelayAddress)
            if splitErr != nil {
                return fmt.Errorf("bad relay address %s: %w", c.Options.RelayAddress, splitErr)
            }
        }
        server := net.JoinHostPort(host, c.Options.RelayPorts[j])
        log.Debugf("connecting to %s", server)
        var connErr error
        c.conn[j+1], _, _, connErr = tcp.ConnectToTCPServer(
            server,
            c.Options.RelayPassword,
            fmt.Sprintf("%s-%d", c.Options.RoomName, j),
        )
        if connErr != nil {
            return fmt.Errorf("connect to port %s: %w", c.Options.RelayPorts[j], connErr)
        }
        log.Debugf("connected to %s", server)
        if !c.Options.IsSender {
            go c.receiveData(j)
        }
        return nil
    })
}
if err = g.Wait(); err != nil {
    if c.stop.gui {
        c.stop.Cancel()
    }
    return err
}
if !c.Options.IsSender {
    log.Debug("sending external IP")
    err = message.Send(c.conn[0], c.Key, message.Message{
        Type:    message.TypeExternalIP,
        Message: c.ExternalIP,
        Bytes:   m.Bytes,
    })
}
return
```

### 4. Check `sync` import usage in the file

The `sync` package is used in other places in the file — lines 145, 266, 923, 924. No need to remove the import.

## Key differences from the original

| Aspect | Before | After |
|--------|--------|-------|
| `SplitHostPort` error | Silent `return` + log | Returned as error |
| `ConnectToTCPServer` error | `panic` — process crash | Returned as error |
| Waiting for goroutines | `wg.Wait()` — unaware of errors | `g.Wait()` — returns first error |
| GUI mode | No handling | `c.stop.Cancel()` for graceful shutdown |
| Local variables | `err` captured from outer function | Local `splitErr`, `connErr` — no data races |

## Why `errgroup.WithContext` is not needed

[`tcp.ConnectToTCPServer`](src/tcp/tcp.go:559) does not accept `context.Context`, so context cancellation on error would not affect other goroutines — they would still be waiting for the connection. Using a simple `errgroup.Group` is sufficient.
