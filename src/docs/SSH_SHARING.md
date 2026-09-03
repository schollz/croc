# SSH terminal sharing

`croc ssh` creates one persistent, collaborative terminal without exposing the
host's normal SSH service. It is intended for pair debugging, support sessions,
demos, and incident response where participants should be able to join with a
short-lived invitation and no prearranged account.

## Host and join

Start an attached session on Linux, macOS, FreeBSD, or OpenBSD:

```bash
croc ssh
```

The host prints two independently generated invitations:

```text
Read/write: CROC_SECRET='...' croc ssh
Read-only:  CROC_SECRET='...' croc ssh
```

Both roles see the same PTY and its existing transcript. Read/write clients and
the attached host may type. Input from a read-only client is consumed without
being sent to the PTY. Any number of clients may attach, subject to the selected
croc relay and the host's resources.

Useful host options:

```bash
croc ssh --headless
croc ssh --duration 30m
croc ssh --dir /path/to/project
```

`--headless` keeps the session available without attaching the host terminal.
The default lifetime is 12 hours. `Ctrl-]` detaches an attached host or guest
while leaving the shared shell alive; `Ctrl-C` in the hosting process stops the
session. A command typed inside the shared terminal, including `exit`, affects
the one shared shell and therefore ends the session for everyone.

For guests, `Ctrl-C` remains normal terminal input and interrupts the foreground
program in the shared shell. Only the locally attached hosting process treats
it as the stop-session shortcut.

Global `--relay` and `--pass` options select a self-hosted croc relay. The host
includes non-default relay settings in both printed join commands, so guests do
not have to reconstruct them separately.

On Unix, pass invitations in `CROC_SECRET` or paste one at the prompt so it does
not appear in the process list. Classic mode permits `croc ssh word-...`, and
Windows accepts that spelling directly. Hosting requires a PTY-capable platform
(Linux, macOS, FreeBSD, or OpenBSD). Those platforms and Windows can join.
Relay-only builds and other targets report that SSH sharing is unsupported.

## Reconnection

After a participant has attached successfully, a transport failure causes the
client to run PAKE again, authorize a fresh transport, verify the SSH host key
again, and reattach to the same PTY. The default retry window is two minutes:

```bash
croc ssh --reconnect-window 10m
croc ssh --no-reconnect
```

Transport selection defaults to `auto`: try Tailcat (direct UDP with DERP
fallback), then reauthenticate over the ordinary croc relay if Tailcat cannot
establish the SSH connection. It can be constrained for diagnostics or network
policy:

```bash
croc ssh --transport tailcat
croc ssh --transport relay
```

There is only one local-terminal reader across reconnects, so an abandoned SSH
copy loop cannot steal input from the replacement connection. Up to 8 MiB of PTY
output is retained in memory and replayed on attachment. If older output was
trimmed, the client receives a warning and a terminal reset before the retained
transcript.

## Protocol and trust model

Each invitation contains six EFF words. The first two derive an opaque relay
room identifier; the remaining four are the PAKE secret. The host and guest run
purpose- and room-bound PAKE with mutual key confirmation over a normal croc
relay room. Relay keepalives are transport frames and are ignored by this
exchange.

After PAKE, the guest sends a fresh Tailcat node public key through the encrypted
control channel. The host returns an authenticated offer containing:

- the persistent session's Tailcat connection blob;
- the exact ephemeral SSH host public key;
- the SSH port and invitation role.

For the primary path, the host allows that Tailcat key and binds its
deterministic tunnel source address to exactly one role. Read/write and
read-only traffic use separate filtered ports, and the role is checked again at
connection dispatch. A grant is consumed by one SSH connection and its Tailcat
key is revoked when that connection closes. Reconnection therefore cannot
bypass PAKE.

Tailcat supplies an accountless userspace WireGuard network. It begins through
a DERP server and upgrades to a direct UDP path when NAT traversal succeeds. If
Tailcat itself cannot establish the SSH connection, the guest performs a fresh
PAKE exchange requesting `relay`; after the authenticated offer, both sides
switch that ordinary croc relay room from framed control messages to the raw,
pinned SSH stream. This fallback works through the same croc relay selected by
the invitation and is identified in the guest's connection message.

The embedded SSH layer provides terminal channel semantics and an independently
encrypted transport on both paths. No host SSH configuration, user password,
authorized key, Tailscale account, or inbound firewall rule is needed.

The embedded SSH service accepts only an interactive PTY session. It does not
accept remote commands, extra channel types, subsystems such as SFTP, agent or
port forwarding, or arbitrary destination ports.

## Security properties and limits

- The croc relay cannot read the invitation secret, Tailcat authorization, SSH
  host key, or terminal stream. It can observe connection timing, the opaque
  rendezvous room, and—when used as the data fallback—the volume and timing of
  SSH ciphertext. A DERP fallback can likewise observe metadata and ciphertext.
- The SSH host key is delivered inside the PAKE-authenticated offer and compared
  byte-for-byte during SSH setup; there is no trust-on-first-use prompt.
- Read-only is enforced before input reaches the shared PTY, not by terminal UI
  convention. A read-only Tailcat source cannot switch to the read/write port.
- Invitation holders have the printed role for the lifetime of the hosting
  process. There is no individual identity, approval prompt, or per-participant
  revocation UI. Stop the host to revoke both invitations immediately.
- Terminal output may contain credentials, command history, environment data,
  or sensitive screen contents. Read-only participants can still see and record
  everything displayed.
- The remote side operates with the privileges of the user who started
  `croc ssh`. The feature does not add a sandbox, container, audit log, or command
  policy. Use a suitably constrained account or environment when that matters.
