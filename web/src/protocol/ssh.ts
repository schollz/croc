import { errorMessage, randomBytes, textEncoder } from "./bytes";
import {
  connectRelay,
  controlPort,
  receiveControl,
  relayAddress,
  sendControl,
} from "./relay";
import type { CrocMessage, TransferSettings } from "./types";
import { wasm } from "../wasm/client";
import { SSHWasmClient, type SSHWorkerEvent } from "../wasm/ssh-client";

const PAKE_PROTOCOL_VERSION = 2;
const SSH_PROTOCOL_VERSION = 2;
const SSH_RENDEZVOUS_FEATURE = "ssh-rendezvous-v1";
const PAKE_PURPOSE_SSH = "peer-ssh";
const PAKE_CURVE = "p256";
const PAKE_SALT_SIZE = 32;
const MAX_CONTROL_SIZE = 1 << 20;
const MAX_PAKE_SIZE = 4 << 10;
const MAX_HOST_KEY_SIZE = 16 << 10;
const MAX_TAILCAT_ADDRESS_SIZE = 512 << 10;
const AUTH_TIMEOUT_MS = 30_000;
const RECONNECT_WINDOW_MS = 2 * 60_000;

export type SSHRole = "read-write" | "read-only";

export type SSHTerminalSize = { width: number; height: number };

export type SSHJoinCallbacks = {
  onStatus?(status: string): void;
  onRole?(role: SSHRole | undefined): void;
  onConnected?(connected: boolean): void;
  onOutput?(bytes: Uint8Array): void | Promise<void>;
  onReconnect?(): void;
};

export type SSHJoinOptions = {
  code: string;
  settings: TransferSettings;
  size: SSHTerminalSize;
  callbacks?: SSHJoinCallbacks;
};

type SSHOffer = {
  hostKey: Uint8Array;
  role: SSHRole;
};

type WorkerClose = { clean: boolean; message: string };

function abortError() {
  return new DOMException("SSH session disconnected", "AbortError");
}

function wait(signal: AbortSignal, delay: number) {
  if (delay <= 0) return Promise.resolve();
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, delay);
    const onAbort = () => {
      window.clearTimeout(timer);
      reject(abortError());
    };
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

function validatePakeMessage(
  message: Awaited<ReturnType<typeof receiveControl>>,
  expected: "pake" | "pake-confirm",
) {
  if (message.t !== expected || message.v !== PAKE_PROTOCOL_VERSION || !message.b) {
    throw new Error(`SSH host sent an invalid ${expected} response`);
  }
  if (message.b.byteLength === 0 || message.b.byteLength > MAX_PAKE_SIZE) {
    throw new Error(`SSH host sent an invalid ${expected} value`);
  }
}

export function validateSSHOffer(message: CrocMessage): SSHOffer {
  if (
    message.t !== "ssh-offer" ||
    message.v !== SSH_PROTOCOL_VERSION ||
    !message.b ||
    !message.m ||
    !message.n ||
    message.f?.length !== 2
  ) {
    throw new Error("SSH host sent an invalid offer");
  }
  const role = message.f[0];
  if (role !== "read-write" && role !== "read-only") {
    throw new Error("SSH host sent an unsupported access role");
  }
  if (message.f[1] !== "relay") {
    throw new Error("SSH host selected an unsupported browser transport");
  }
  const expectedPort = role === "read-write" ? 22 : 23;
  if (message.n !== expectedPort) {
    throw new Error("SSH host sent an inconsistent role and port");
  }
  if (
    message.b.byteLength === 0 ||
    message.b.byteLength > MAX_HOST_KEY_SIZE ||
    textEncoder.encode(message.m).byteLength > MAX_TAILCAT_ADDRESS_SIZE
  ) {
    throw new Error("SSH host sent an incomplete offer");
  }
  return { hostKey: message.b, role };
}

async function authorize(
  code: string,
  settings: TransferSettings,
  signal: AbortSignal,
) {
  const engine = wasm();
  const components = await engine.sshCodeComponents(code);
  if (settings.relayAddresses.length === 0) {
    throw new Error("No croc relay is configured");
  }
  const relayIndex = await engine.relayIndex(code, settings.relayAddresses.length);
  const relay = await connectRelay(
    settings,
    components.room,
    controlPort(relayAddress(settings, relayIndex)),
    relayIndex,
    signal,
  );
  const socket = relay.socket;
  let trafficKey: Uint8Array | undefined;
  let clientAuth: Uint8Array | undefined;
  try {
    const initiator = await engine.pakeInitWithIdentities(
      textEncoder.encode(components.passphrase),
      0,
      PAKE_CURVE,
      PAKE_PURPOSE_SSH,
      components.room,
    );
    await sendControl(socket, {
      t: "pake",
      v: PAKE_PROTOCOL_VERSION,
      b: initiator.bytes,
      b2: textEncoder.encode(PAKE_CURVE),
      f: [SSH_RENDEZVOUS_FEATURE],
    });
    const response = await receiveControl(socket, undefined, MAX_CONTROL_SIZE);
    validatePakeMessage(response, "pake");
    if (!response.b2 || response.b2.byteLength !== PAKE_SALT_SIZE) {
      throw new Error("SSH host sent an invalid PAKE salt");
    }
    const finished = await engine.pakeUpdate(initiator.handle, response.b!);
    const keys = await engine.derivePeerKeys(
      finished.key,
      response.b2,
      PAKE_PURPOSE_SSH,
      components.room,
      PAKE_CURVE,
      initiator.bytes,
      response.b!,
    );
    trafficKey = keys.key;
    await sendControl(socket, {
      t: "pake-confirm",
      v: PAKE_PROTOCOL_VERSION,
      b: keys.confirmationA,
    });
    const confirmation = await receiveControl(
      socket,
      undefined,
      MAX_CONTROL_SIZE,
    );
    validatePakeMessage(confirmation, "pake-confirm");
    if (!(await engine.confirmPeerKey(keys.confirmationB, confirmation.b!))) {
      throw new Error("SSH host failed PAKE key confirmation");
    }

    const compatibilityKey = randomBytes(32);
    if (compatibilityKey.every((byte) => byte === 0)) compatibilityKey[0] = 1;
    clientAuth = randomBytes(32);
    if (clientAuth.every((byte) => byte === 0)) clientAuth[0] = 1;
    socket.prepareRawHandoff();
    await sendControl(
      socket,
      {
        t: "ssh-authorize",
        v: SSH_PROTOCOL_VERSION,
        b: compatibilityKey,
        b2: clientAuth,
        f: ["relay"],
      },
      trafficKey,
    );
    compatibilityKey.fill(0);
    const offer = validateSSHOffer(
      await receiveControl(socket, trafficKey, MAX_CONTROL_SIZE),
    );
    return { socket, offer, clientAuth };
  } catch (error) {
    clientAuth?.fill(0);
    socket.close();
    throw error;
  } finally {
    trafficKey?.fill(0);
  }
}

export class SSHJoinSession {
  readonly done: Promise<void>;

  private readonly controller = new AbortController();
  private active:
    | { worker: SSHWasmClient; handle: number; role: SSHRole }
    | undefined;
  private size: SSHTerminalSize;
  private inputQueue = Promise.resolve();
  private attached = false;
  private disconnectedAt = 0;
  private failedAttempts = 0;

  constructor(private readonly options: SSHJoinOptions) {
    this.size = options.size;
    this.done = this.run();
  }

  sendInput(bytes: Uint8Array) {
    const active = this.active;
    if (!active || active.role === "read-only" || this.controller.signal.aborted) {
      return;
    }
    this.inputQueue = this.inputQueue
      .then(() => active.worker.input(active.handle, bytes))
      .catch(() => this.disconnect());
  }

  resize(size: SSHTerminalSize) {
    if (size.width <= 0 || size.height <= 0) return;
    this.size = size;
    const active = this.active;
    if (active) {
      void active.worker
        .resize(active.handle, size.width, size.height)
        .catch(() => {});
    }
  }

  disconnect() {
    if (this.controller.signal.aborted) return;
    this.controller.abort();
    const active = this.active;
    if (active) void active.worker.stop(active.handle);
  }

  private async run() {
    try {
      for (;;) {
        if (this.controller.signal.aborted) throw abortError();
        this.options.callbacks?.onStatus?.(
          this.attached ? "Reconnecting to shared terminal…" : "Connecting to relay…",
        );
        try {
          const clean = await this.runAttempt();
          if (clean) {
            this.options.callbacks?.onStatus?.("Shared terminal ended");
            return;
          }
          throw new Error("SSH connection closed unexpectedly");
        } catch (error) {
          if (this.controller.signal.aborted) throw abortError();
          if (!this.attached) throw error;
          if (!this.disconnectedAt) this.disconnectedAt = Date.now();
          if (Date.now() - this.disconnectedAt >= RECONNECT_WINDOW_MS) {
            throw new Error(`SSH reconnect window expired: ${errorMessage(error)}`);
          }
          this.failedAttempts += 1;
          this.options.callbacks?.onConnected?.(false);
          this.options.callbacks?.onReconnect?.();
          await wait(
            this.controller.signal,
            Math.min(Math.max(0, this.failedAttempts - 1) * 500, 5_000),
          );
          continue;
        } finally {
          if (this.active) {
            await this.active.worker.stop(this.active.handle);
            this.active.worker.dispose();
            this.active = undefined;
          }
        }
      }
    } finally {
      this.options.callbacks?.onConnected?.(false);
      this.options.callbacks?.onRole?.(undefined);
      this.active = undefined;
    }
  }

  private async runAttempt() {
    const authController = new AbortController();
    const abortAuth = () => authController.abort();
    this.controller.signal.addEventListener("abort", abortAuth, { once: true });
    const authTimer = window.setTimeout(() => authController.abort(), AUTH_TIMEOUT_MS);
    let socket: Awaited<ReturnType<typeof authorize>>["socket"] | undefined;
    let worker: SSHWasmClient | undefined;
    let handle: number | undefined;
    let role: SSHRole | undefined;
    let clientAuth: Uint8Array | undefined;
    let connected = false;
    let networkFailure: unknown;
    let closeSession!: (value: WorkerClose) => void;
    const closed = new Promise<WorkerClose>((resolve) => {
      closeSession = resolve;
    });

    try {
      this.options.callbacks?.onStatus?.("Authenticating invitation…");
      const authorization = await authorize(
        this.options.code,
        this.options.settings,
        authController.signal,
      );
      socket = authorization.socket;
      clientAuth = authorization.clientAuth;
      role = authorization.offer.role;
      this.options.callbacks?.onRole?.(role);
      this.options.callbacks?.onStatus?.("Starting encrypted SSH session…");

      const onWorkerEvent = async (event: SSHWorkerEvent) => {
        if (event.type === "network") {
          await socket!.sendRaw(event.data);
        } else if (event.type === "output") {
          await this.options.callbacks?.onOutput?.(event.data);
        } else if (event.type === "connected") {
          connected = true;
          this.attached = true;
          this.disconnectedAt = 0;
          this.failedAttempts = 0;
          window.clearTimeout(authTimer);
          this.options.callbacks?.onConnected?.(true);
          this.options.callbacks?.onStatus?.(
            `Connected with ${role} access via the croc relay`,
          );
        } else if (event.type === "closed") {
          closeSession({ clean: event.clean, message: event.message });
        }
      };
      worker = new SSHWasmClient(onWorkerEvent);
      handle = await worker.start(
        authorization.offer.hostKey,
        authorization.clientAuth,
        this.size.width,
        this.size.height,
      );
      this.active = { worker, handle, role };

      void (async () => {
        try {
          for (;;) {
            await worker!.feed(handle!, await socket!.receiveRaw());
          }
        } catch (error) {
          networkFailure = error;
          if (worker && handle !== undefined) {
            await worker.stop(handle).catch(() => {});
          }
        }
      })();

      const outcome = await closed;
      if (outcome.clean) return true;
      if (networkFailure) throw networkFailure;
      throw new Error(outcome.message || "SSH connection closed");
    } catch (error) {
      if (authController.signal.aborted && !this.controller.signal.aborted && !connected) {
        throw new Error("SSH authentication or handshake timed out");
      }
      throw error;
    } finally {
      window.clearTimeout(authTimer);
      this.controller.signal.removeEventListener("abort", abortAuth);
      socket?.close();
      clientAuth?.fill(0);
      if (worker) {
        if (handle !== undefined) await worker.stop(handle);
        worker.dispose();
      }
      if (this.active?.worker === worker) this.active = undefined;
    }
  }
}
