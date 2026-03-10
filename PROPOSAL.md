# Federated Relay Pool for Croc - Architecture and Implementation Notes

**Status:** Implemented in v10.5.0  
**Last Updated:** 2026-03-09 | **Revision:** 2.3 (Security-hardened)

---

## Executive Summary

A lightweight relay **discovery + coordination** system that:

- Allows community operators to register relays with the **Main Node** (pool)
- Embeds relay IDs directly into share codes: `12ca-1571-yellow-apple`
- Eliminates complex room coordination overhead
- Maintains backward compatibility with existing TCP relay infrastructure
- Keeps pool APIs minimal and stateless

---

## Architecture Overview

### Components

```
┌─────────────────────────────────────────────────┐
│         Main Node (Pool + Legacy TCP)        │
│  - HTTP/REST API: /register, /heartbeat, /relays│
│  - Relay Registry & Health Checks               │
│  - TCP Relay (9009+) for legacy fallback        │
└─────────────────────────────────────────────────┘
         ↑                  ↓
    (register/          (discover/
     heartbeat)          cache)
         ↑                  ↓
  ┌──────────────────────────────────────┐
  │  Community Relays (TCP servers)      │
  │  - Relay#102: 203.0.113.5:9009       │
  │  - Relay#103: 198.51.100.8:9009      │
  │  - Relay#104: 192.0.2.15:9009        │
  └──────────────────────────────────────┘
         ↑
    (heartbeat)
         ↑
  ┌──────────────────────────────────────┐
  │   Croc Clients (Sender/Receiver)     │
  │  - Cache: relay_id → IP:ports        │
  │  - Generate: relay_id + secret       │
  │  - Connect: Direct TCP to relay      │
  └──────────────────────────────────────┘
```

### Data Flow

**Sender (croc send file.txt):**

```
1. POST /relays → List of available {relay_id, ipv6, ipv4, ports, status, version} (**no password** — credentials are out-of-band)
2. Cache relay registry locally
3. Select relay (fastest, random, or preference)
4. Generate code: "12ca-1571-yellow-apple"
5. Connect TCP directly to relay (IPv6 first, fallback IPv4) using ports[0]
6. Proceed with standard croc handshake
7. On SIGINT/done: no explicit cleanup needed
```

**Receiver (croc 12ca-1571-yellow-apple):**

```
1. Parse relay_id = "12ca" from code
2. Lookup relay_id in local cache → IPv6/IPv4 + ports
3. If not cached: POST /relays (fallback lookup)
4. Connect TCP directly to relay (IPv6 first, fallback IPv4) using ports[0]
5. Proceed with standard croc handshake
```

**Community Relay Lifecycle:**

```
1. croc relay [--pool <pool_url>] [--ports 9009,9010,9011,9012,9013] node
2. Detect public IP address (IPv6 preferred, IPv4 fallback)
3. Calculate relay_id = SHA256(ip)[:2] as hex (4 chars)
4. POST /register {ipv6, ipv4, ports, password}
	→ Returns confirmation
5. Loop every 10s: POST /heartbeat {relay_id}
6. Main Node tracks: {relay_id → state, last_heartbeat}
7. If heartbeat missing 30s+: mark Inactive
8. On shutdown (SIGINT): cleanup heartbeat goroutine
9. On restart: relay_id auto-regenerated from IP (no storage needed)

If `--pool` is omitted, node mode uses the global default pool URL (http://croc.schollz.com:9000).
If `--ports` is omitted, relay defaults are used (`9009,9010,9011,9012,9013`).
```

**Relay CLI Modes (proposed):**

```
1. croc relay                                    -> Legacy standalone relay (no pool)
2. croc relay main                               -> Pool main node
3. croc relay [--pool <url>] [--ports ...] node -> Pool community relay node
4. if --pool is omitted, use internal default global pool constant
5. if --ports is omitted, use relay defaults (9009-9013)
```

### Concrete CLI Examples

**A) Backward-compatible standalone relay (no pool):**

```bash
# same behavior as today
croc relay

# custom standalone relay ports
croc relay --ports 9009,9010,9011,9012,9013 --pass myrelaypass
```

**B) Private pool (default in pool modes):**

```bash
# 1) Start private pool main
croc relay main --listen 10.0.0.10:9000

# 2) Join first private pool node (uses default ports 9009-9013)
croc relay --pool http://10.0.0.10:9000 --pass privatepass node

# 3) Join second private pool node
croc relay --pool http://10.0.0.10:9000 --ports 9109,9110,9111,9112,9113 --pass privatepass node

# 4) Client uses private pool for relay discovery
croc --pool http://10.0.0.10:9000 send file.txt
```

**C) Public pool:**

```bash
# 1) Start public pool main
croc relay main --listen 0.0.0.0:9000

# 2) Join global default public pool node #1
# Note: --pool optional since http://croc.schollz.com:9000 is default
croc relay --pass pass123 node

# 3) Join specific non-default public pool node #2
# Note: --pool required when NOT using default pool
croc relay --pool http://pool.example.com:9000 --ports 9109,9110,9111,9112,9113 --pass pass123 node

# 4) Client uses default global public pool
# Note: no --pool flag needed, uses built-in default
croc send file.txt

# 5) Client targets a non-default public pool explicitly
# Note: --pool required when NOT using default pool
croc --pool http://pool.example.com:9000 send file.txt
```

**E) Client copy-paste quick commands:**

```bash
# default global pool (internal constant)
croc send file.txt
croc 12ca-1571-yellow-apple

# non-default pool override (private or public)
croc --pool http://10.0.0.10:9000 send file.txt
croc --pool http://10.0.0.10:9000 12ca-1571-yellow-apple
```

**D) Client fallback remains unchanged:**

```bash
# If pool is unavailable or code has no relay prefix,
# clients can still use classic direct relay addressing.
croc --relay myrelay.example.com:9009 --pass myrelaypass send file.txt
croc --relay myrelay.example.com:9009 --pass myrelaypass <code>
```

---

## Implementation Overview

### Phase 1: Core State Management

Create a new pool coordination module that manages relay registry and state:

**Relay State Tracking:**

- Each relay has a unique 4-character hex ID (derived from IP hash)
- Track both IPv6 (primary) and IPv4 (fallback) addresses
- Monitor relay health through status (active/inactive)
- Record last heartbeat timestamp
- Store relay ports and optional password

**Registry Requirements:**

- Thread-safe concurrent access to relay data
- Support upsert operations (register/update)
- Query relays by ID
- List active relays with randomization
- Update heartbeat timestamps
- Mark relays as inactive
- Periodic cleanup of stale relays (30 second timeout)

**Key Concepts:**

- Relay ID must be deterministic based on IP address
- IPv6 addresses take priority over IPv4
- Relay IDs are deterministically regenerated from public IP on each registration
- Inactive relays remain in registry but aren't distributed to clients

---

### Phase 2: Helper Utilities

Provide support functions for relay pool operations:

**IP Address Handling:**

- Generate deterministic relay IDs from IP addresses using hash algorithm (SHA256, first 16 bits → 4 hex chars)
- Relay ID is deterministic: same IP always produces same relay_id
- **No storage needed on relay nodes**: relay_id recalculated from IP on each registration
- Validate public IPv6 addresses (exclude loopback, private, link-local, multicast)
- Validate public IPv4 addresses (exclude RFC 1918, CGNAT, loopback, link-local)
- Prefer IPv6 over IPv4 when both available

**Connectivity Testing:**

- Check if relay is reachable via TCP connection
- Try IPv6 first, fallback to IPv4
- Use short timeout (1 second) to avoid blocking
- Detect if local system supports IPv6

**Code Parsing:**

- Detect if code contains relay ID prefix (4-char hex)
- Extract relay_id and secret from combined code format: `<relay_id>-<secret>`
- Handle legacy codes without relay ID prefix
- **Critical**: Room name must be derived from secret part AFTER relay_id, not from relay_id itself

---

### Phase 3: Pool HTTP API

Implement a lightweight HTTP REST API for relay coordination:

**Three Core Endpoints:**

**1. POST /register** - Community relay registration

- Accept relay connection details (IPv6/IPv4, ports, password)
- Require at least one IP address (IPv6 or IPv4)
- Require minimum 2 ports
- Calculate deterministic relay_id from IP (SHA256 hash, first 2 bytes)
- Store relay state as active with current timestamp
- Return assigned relay_id for confirmation
- Support upsert behavior (re-registration with same ID)

**2. POST /heartbeat** - Keep relay alive

- Accept relay ID
- Update relay's last heartbeat timestamp
- Mark relay as active
- Return success confirmation
- Return error if relay ID not found

**3. POST /relays** - List active relays for clients

- Return array of up to 50 active relays
- Include relay ID, IPv6/IPv4 addresses, ports, status, node version (**password is never returned** — clients must obtain relay credentials out-of-band)
- Randomize order to distribute load
- Only include relays marked as active

**Background Tasks:**

- Run periodic cleanup every 30 seconds (equal to heartbeat timeout, to minimise stale-relay exposure)
- Mark relays as inactive if no heartbeat for 30+ seconds
- Cleanup also runs eagerly before each /relays response
- Keep inactive relays in registry for potential recovery

**Server Configuration:**

- Listen on configurable address (default: 0.0.0.0:9000)
- Use HTTP web framework for routing
- Log all registration and heartbeat events

---

### Phase 4: Relay Command Enhancement

Extend the existing relay command to support pool federation:

**Three Relay Modes:**

**Mode 1: croc relay** (Legacy Standalone)

- Maintain existing behavior unchanged
- Start TCP relay without any pool interaction
- Fully backward compatible
- No pool coordination

**Mode 2: croc relay main** (Pool Main Node)

- Start HTTP pool API server
- Manage relay registry and health monitoring
- Provide relay discovery for clients
- Listen on configurable address (--listen flag)
- Optional: Also run legacy TCP relay for fallback

**Mode 3: croc relay [flags] node** (Community Relay Node)

- Start standard TCP relay on specified ports
- Register with pool on startup
- Send periodic heartbeats every 10 seconds
- Use server-assigned deterministic relay ID derived from public IP
- Auto-reconnect to pool on network failures

**Community Node Workflow:**

1. **Configuration Resolution:**
   - Pool URL: CLI flag → environment variable → default
   - Ports: CLI flag → defaults (9009-9013)
   - Default pool: http://croc.schollz.com:9000
   - Note: `--pool` optional when using default

2. **IP Address Detection:**
   - Detect public IPv6 address (preferred)
   - Detect public IPv4 address (fallback)
   - At least one public IP required
   - Calculate relay_id from IP: `SHA256(ip)[:2]` as hex

3. **Startup Sequence:**
   - Detect public IP and build register payload
   - Register with pool to obtain relay_id
   - Begin heartbeat loop
   - Start TCP relay listeners on all ports

4. **Registration + Heartbeat Loop:**
   - POST to /register with connection details
   - Receive relay_id from main node
   - POST to /heartbeat every 10 seconds
   - Log failures but continue retrying
   - Don't crash on temporary network issues

5. **Graceful Shutdown:**
   - Stop heartbeat loop on SIGINT
   - Close TCP listeners
   - No cleanup needed (relay_id recalculated on next start)

---

### Phase 5: Client Integration

Enhance croc clients to support federated relay discovery:

**Client-Side Relay Cache:**

- Maintain local cache mapping relay IDs to connection info
- Cache includes: IPv6/IPv4 addresses, ports, node version (passwords not cached from pool; known out-of-band)
- Allow pool URL override via config

**Relay Discovery Workflow:**

- Query pool: POST to /relays endpoint
- Receive list of active relays
- Store in local cache for session
- Cache remains valid until client restart
- On cache miss, re-query pool

**Sender Workflow (croc send file.txt):**

1. **Determine Relay Mode:**
   - If `--relay` explicitly set: use direct relay (legacy mode, skip pool)
   - If `--pool` explicitly set: use that pool URL
   - If `CROC_POOL_URL` env var set: use that pool
   - Else: try default pool (http://croc.schollz.com:9000)
   - Note: `--pool` flag optional when using default pool

2. **Query Pool for Relays (if pool mode):**
   - POST to /relays endpoint
   - Cache all returned relays locally for this session
   - If pool unreachable: fallback to default relay (legacy mode)

3. **Select Best Relay:**
   - Test connectivity to cached relays (1 second timeout)
   - Build list of reachable relays
   - Select relay (random, first, or lowest latency)
   - If all fail, fallback to default relay

4. **Generate Federated Code:**
   - Call `utils.GetRandomName(relay_id)` with selected relay's ID
   - Format: `<relay_id>-<pin>-<words>`
   - Example: `12ca-1571-yellow-apple`
   - If no pool relay selected, use legacy format: `utils.GetRandomName("")`
   - **Important**: Code generation happens AFTER relay selection

5. **Connect to Selected Relay:**
   - Use IPv6 address if available
   - Fallback to IPv4 if IPv6 unavailable
   - Use relay's password from cache

**Receiver Workflow (croc 12ca-1571-yellow-apple):**

1. **Parse Transfer Code:**
   - Check if first segment (before first dash) is 4-char hex pattern
   - If yes: `relay_id = "12ca"`, `secret = "1571-yellow-apple"`
   - If no: treat entire code as secret (legacy mode)

2. **Generate Room Name:**
   - **Critical**: Room name derived from secret part, NOT relay_id
   - `roomName = SHA256(secret[:4] + hashExtra)`
   - In example: `roomName = SHA256("7123" + "croc")`
   - This ensures unique rooms per transfer, not per relay

3. **Lookup Relay Information:**
   - If relay_id found, check local cache
   - If not in cache, query pool: POST /relays, find matching relay_id
   - Pool URL resolved from: `--pool` flag → `CROC_POOL_URL` env → default
   - If using default pool, `--pool` flag not required
   - If relay not found anywhere, fallback to default relay

4. **Connect to Relay:**
   - Use cached relay connection info (IP, ports, password)
   - Try IPv6 first, fallback to IPv4
   - Apply relay password from cache
   - Use room name derived from secret (not relay_id)
   - Log connection details

**Fallback Strategy:**

- If pool unreachable: use hardcoded default relay
- If relay_id not found: use default relay
- If relay connection fails: timeout and report error
- Legacy codes (no relay_id) always work via default relay

---

### Phase 6: Configuration & Integration

**Required Dependencies:**

- HTTP web framework for REST API implementation
- Thread-safe concurrent map for relay registry
- Standard libraries for networking, JSON, hashing

**Configuration Constants:**

- Default pool URL: http://croc.schollz.com:9000 (built-in constant)
- Default relay address: croc.schollz.com:9009 (legacy fallback)
- Relay status values: "active", "inactive"
- Heartbeat timeout: 30 seconds
- Heartbeat interval: 10 seconds
- Relay ID length: 4 hex characters (2 bytes from SHA256)
- Default ports: 9009,9010,9011,9012,9013

**Environment Variables:**

_Pool Server:_

- CROC_POOL_LISTEN - API listen address (e.g., "0.0.0.0:9000")

_Relay Nodes:_

- CROC_POOL_URL - Pool URL for registration (e.g., "http://croc.schollz.com:9000")
- CROC_RELAY_PORTS - Port list override (e.g., "9009,9010,9011,9012,9013")

_Clients:_

- CROC_POOL_URL - Pool URL for relay discovery (same variable as relay nodes)
- CROC_RELAY - Direct relay address, disables pool mode (legacy, still supported)
- CROC_RELAY6 - Direct IPv6 relay address (legacy, still supported)

**Note:** Default pool URL is built-in constant, so CROC_POOL_URL only needed for non-default pools

**Configuration Priority:**

1. CLI flags (highest)
2. Environment variables
3. Default constants (lowest)

**Relay ID Generation (No Storage Needed):**

- Relay ID is deterministically calculated from relay's public IP address
- Hash algorithm: `relay_id = hex(SHA256(ip_address)[:2])` → 4 hex chars
- Same IP always produces same relay_id (idempotent)
- **No `.relay_id` file needed on relay nodes** - just recalculate from IP
- On restart, relay detects its public IP and regenerates same relay_id
- This ensures transfer codes remain valid after relay restarts
- Pool handles re-registration (upsert behavior) automatically

---

## Simplified HTTP Model

1. Pool exposes only three endpoints: `/register`, `/heartbeat`, `/relays`.
2. No token auth, no signature layer, and no TTL response fields.
3. Relay state is binary: `active` or `inactive`.
4. Heartbeats only refresh `last_heartbeat` and keep status `active`.
5. Missing heartbeat beyond timeout marks relay `inactive`.

---

## End-to-End Walkthrough

### Scenario: Federated Transfer

**Setup:**

- Main Node Pool: `croc.schollz.com:9000` (HTTP API) + `:9009` (legacy TCP fallback)
- Community Relay#102: `203.0.113.5` (owns IP, ports 9009-9013)
- Community Relay#103: `198.51.100.8` (owns IP, ports 9009-9013)

**Execution:**

1. **Community Relay boots (first time):**

   ```bash
   $ croc relay --pool http://croc.schollz.com:9000 --pass relay_pass node

   [INFO] Starting TCP relay on 203.0.113.5:9009...
   [INFO] Registering with pool...
   [INFO] Detected public IP: 203.0.113.5
   [INFO] Assigned relay_id: 12ca
   [INFO] Registered as relay#12ca
   [INFO] Sending heartbeats every 10s
   ```

**Pool sees:**

- Relay assigned ID: `12ca` (deterministic from IP hash)
- Status: `active`

2. **Community Relay restarts (crash/maintenance):**

   ```bash
   $ croc relay --pool http://croc.schollz.com:9000 node ...

   [INFO] Starting TCP relay on 203.0.113.5:9009...
   [INFO] Detected public IP: 203.0.113.5
   [INFO] Assigned relay_id from pool: 12ca
   [INFO] Re-registering with pool...
   [INFO] Relay#12ca re-activated (same ID as before)
   ```

   **Key insight:** Relay_id deterministically derived from IP → no storage needed, existing codes always work!

3. **Sender initiates transfer:**

   ```bash
   $ croc send largefile.bin

   [INFO] Querying pool for available relays...
   [INFO] Got: relay#12ca (203.0.113.5), relay#44bd (198.51.100.8)
   [INFO] Cached both locally
   [INFO] Latency check: 12ca→45ms, 44bd→120ms
   [INFO] Selected relay#12ca
   [INFO] Generated code: 12ca-1571-yellow-apple

   Code is: 12ca-1571-yellow-apple
   On the other computer run:
      croc 12ca-1571-yellow-apple
   ```

4. **Receiver joins (different computer, later):**

   ```bash
   $ croc 12ca-1571-yellow-apple

   [INFO] Parsed code: relay_id=12ca, secret=1571-yellow-apple
   [INFO] Checking relay cache...
   [INFO] Not in cache, querying pool... (if no previous transfer)
   [INFO] Found relay#12ca → 203.0.113.5:9009
   [INFO] Connecting to 203.0.113.5:9009...
   [INFO] Connected! Waiting for sender...
   ```

5. **Transfer completes:**

   ```
   largefile.bin (125 MB) 100% |████████████| [5s, 25MB/s]
   Received
   ```

6. **Cleanup:**
   - Sender/Receiver close TCP connections
   - Room auto-expires on relay (no explicit cleanup needed)
   - Community Relay continues serving, sends next heartbeat
   - Relay_id remains stable (deterministic from IP) for next boot

---

## Implementation Checklist

**Phase 1: Helper Utilities** (Foundation)

- [x] Create `src/pool/utils.go`
- [x] Implement relay ID generation from IP hash (deterministic)
- [x] Implement public IP validation (IPv4/IPv6)
- [x] Implement code parsing (detect relay_id prefix)
- [x] Modify `src/utils/utils.go::GetRandomName(relay_id string)` to accept optional relay_id
- [x] Add tests for code generation with/without relay_id

**Phase 2: Core State Management** (Pool Registry)

- [x] Create `src/pool/registry.go`
- [x] Implement thread-safe relay registry (concurrent map)
- [x] Add relay state tracking (active/inactive)
- [x] Implement upsert operations (register/update)
- [x] Implement query by relay_id
- [x] Implement list active relays with randomization
- [x] Add periodic cleanup mechanism (30s timeout)

**Phase 3: Pool HTTP API** (Server)

- [x] Create `src/pool/pool.go`
- [x] Implement POST /register endpoint
- [x] Implement POST /heartbeat endpoint
- [x] Implement POST /relays endpoint
- [x] Add background cleanup goroutine
- [x] Add request validation and error handling
- [x] Add logging for all operations

**Phase 4: Relay Command Enhancement** (Node Mode)

- [x] Modify `src/cli/cli.go::relay()` function
- [x] Add subcommand detection (main/node)
- [x] Implement "croc relay main" (pool server mode)
- [x] Implement "croc relay [flags] node" (community relay mode)
- [x] Add flags: --pool, --public, --listen
- [x] Implement registration/heartbeat loop in node mode
- [x] Add public IP detection for relay nodes
- [x] Test pool server startup and relay registration

**Phase 5: Client Integration** (Send/Receive)

- [x] Create `src/pool/client.go` for pool queries
- [x] Add relay cache to client struct
- [x] Implement relay mode detection logic
- [x] Modify `src/cli/cli.go::send()` to query pool before code generation
- [x] Modify sender to call `GetRandomName()` with relay_id after selection
- [x] Modify `src/croc/croc.go::New()` to parse relay_id from codes
- [x] Fix room name generation to use secret part (not relay_id)
- [x] Modify receiver to query pool for relay lookup
- [x] Add IPv6-first connection logic
- [x] Implement fallback strategies (pool → default relay)

**Phase 6: Testing & Validation**

- [x] Test pool server startup and API endpoints
- [x] Test community relay registration (first time)
- [x] Test relay restart with same IP (relay_id regeneration)
- [x] Test heartbeat mechanism and timeout
- [x] Test sender: pool query → relay selection → code generation flow
- [x] Test receiver: code parsing → pool lookup → connection
- [x] Test relay_id determinism (same IP = same ID)
- [x] Test room name generation (uniqueness per transfer)
- [x] Test IPv6/IPv4 fallback
- [x] Test pool unavailability fallback to default relay
- [x] Test legacy codes (no relay_id prefix)
- [x] Test backward compatibility with old clients

**Phase 7: Documentation & Deployment**

- [x] Update README with pool architecture overview
- [x] Document new CLI commands (relay main, relay node)
- [x] Document all environment variables (CROC_POOL_URL, etc.)
- [x] Create usage examples for private and public pools
- [ ] Write migration guide for existing relay operators
- [x] Document code format change (relay_id prefix)
- [ ] Add troubleshooting guide (pool connection failures, etc.)

---

## Backward Compatibility

✅ **Fully maintained:**

- Legacy codes (no relay ID prefix) work as before
- Default relay `croc.schollz.com:9009` still available
- Existing standalone relay workflow remains: `croc relay`
- Existing `--relay` flag respected
- Zero changes to core TCP relay protocol
- Versionless pool API (no version header required)

✅ **Graceful degradation:**

- If pool unreachable: use hardcoded relay
- If selected relay fails: timeout & fallback to default
- If relay_id not found: use default relay
- If main node reboots: relay_id re-registers from deterministic IP hash, codes remain valid

✅ **No BREAKING changes:**

- Existing clients unaffected
- Relay_id embedding is optional (codes work with or without prefix)
- Pool API backward compatible (no versioning discipline)

---

## References

- **croc GitHub:** https://github.com/schollz/croc
- **Go net/http Docs:** https://pkg.go.dev/net/http
