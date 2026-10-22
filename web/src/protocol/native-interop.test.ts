// @vitest-environment node
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  execFileSync,
  spawn,
  type ChildProcessWithoutNullStreams,
} from "node:child_process";
import { createInterface } from "node:readline";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { mkdtempSync, rmSync } from "node:fs";
import { ProtocolOperations } from "./operations";
import vectors from "../../testdata/native-vectors.json";
import { Pake } from "./pake";
import { XXH64 } from "./xxh64";
import { codeComponents, relayIndex, sshCodeComponents } from "../codephrase";

let process: ChildProcessWithoutNullStreams;
let directory: string;
const pending: Array<{
  resolve(value: unknown): void;
  reject(error: Error): void;
}> = [];
beforeAll(() => {
  directory = mkdtempSync(join(tmpdir(), "croc-oracle-"));
  const binary = join(directory, "oracle");
  execFileSync("go", ["build", "-o", binary, "./testdata/oracle"], {
    cwd: import.meta.dirname + "/../..",
  });
  process = spawn(binary, [], { stdio: "pipe" });
  createInterface({ input: process.stdout }).on("line", (line) => {
    const response = JSON.parse(line),
      next = pending.shift()!;
    if (response.error) next.reject(new Error(response.error));
    else next.resolve(response.result);
  });
  process.on("exit", () => {
    for (const p of pending.splice(0)) p.reject(new Error("Oracle exited"));
  });
}, 30000);
afterAll(() => {
  process?.kill();
  if (directory) rmSync(directory, { recursive: true, force: true });
});
function oracle<T>(
  Method: string,
  fields: Record<string, unknown> = {},
): Promise<T> {
  return new Promise((resolve, reject) => {
    pending.push({ resolve: resolve as (value: unknown) => void, reject });
    process.stdin.write(
      JSON.stringify({ Method, ...fields }, (_key, value) =>
        value instanceof Uint8Array
          ? Buffer.from(value).toString("base64")
          : value,
      ) + "\n",
    );
  });
}
const decode = (value: string) => new Uint8Array(Buffer.from(value, "base64"));
const password = new TextEncoder().encode("correct horse 🐊");
type Start = { handle: number; bytes: string };
type Finish = { bytes: string; key: string };
const engine = new ProtocolOperations();

describe("Go ↔ native protocol", () => {
  it("matches the shared, fixed Go vectors", async () => {
    for (const { request: q, expected } of vectors) {
      const bytes = (value?: string) => decode(value ?? "");
      switch (q.Method) {
        case "derive":
          expect(
            await engine.deriveKey(bytes(q.Password), bytes(q.Salt)),
          ).toEqual(bytes(expected as string));
          break;
        case "peer": {
          const actual = await engine.derivePeerKeys(
            bytes(q.Key),
            bytes(q.Salt),
            q.Purpose!,
            q.Room!,
            q.Curve!,
            bytes(q.Initiator),
            bytes(q.Responder),
          );
          for (const key of ["key", "confirmationA", "confirmationB"] as const)
            expect(actual[key]).toEqual(
              bytes((expected as unknown as Record<string, string>)[key]),
            );
          break;
        }
        case "seal":
          expect(
            await engine.decrypt(bytes(expected as string), bytes(q.Key)),
          ).toEqual(bytes(q.Bytes));
          break;
        case "hashPattern": {
          const hash = new XXH64();
          hash.update(
            Uint8Array.from(
              { length: q.Count! },
              (_, i) => (i * 37 + (i % 31)) % 256,
            ),
          );
          expect(hash.digest()).toEqual(bytes(expected as string));
          break;
        }
        case "code":
          expect(await codeComponents(q.Code!)).toEqual(expected);
          break;
        case "relay":
          expect(await relayIndex(q.Code!, q.Count!)).toEqual(expected);
          break;
      }
    }
  });
  for (const curve of ["p256", "p384", "p521", "siec", "ed25519"]) {
    for (const purpose of ["", "peer-transfer", "local-ip-probe", "peer-ssh"]) {
      for (const role of [0, 1] as const) {
        it(`${curve} ${purpose || "relay"}, JavaScript role ${role}`, async () => {
          const room = "test-room",
            remote = await oracle<Start>("init", {
              Password: password,
              Role: 1 - role,
              Curve: curve,
              Purpose: purpose,
              Room: room,
            });
          const local = new Pake(
            password,
            role,
            curve,
            purpose || undefined,
            room,
          );
          const first = local.bytes();
          let go: Finish, js: { bytes: Uint8Array; key: Uint8Array };
          if (role === 0) {
            go = await oracle<Finish>("update", {
              Handle: remote.handle,
              Bytes: first,
            });
            js = await local.update(decode(go.bytes));
          } else {
            js = await local.update(decode(remote.bytes));
            go = await oracle<Finish>("update", {
              Handle: remote.handle,
              Bytes: js.bytes,
            });
          }
          expect(js.key).toEqual(decode(go.key));
          // Go preserves all public fields in its update; reconstructing JSON must be byte-identical.
          expect(first).toEqual(decode(await oracle("wire", { Bytes: first })));
          expect(js.bytes).toEqual(
            decode(await oracle("wire", { Bytes: js.bytes })),
          );
          if (purpose) {
            const salt = Uint8Array.from({ length: 32 }, (_, i) => i);
            const initiator = role === 0 ? first : decode(remote.bytes);
            const responder = role === 0 ? decode(go.bytes) : js.bytes;
            const args = {
              Key: js.key,
              Salt: salt,
              Purpose: purpose,
              Room: room,
              Curve: curve,
              Initiator: initiator,
              Responder: responder,
            };
            const expected = await oracle<Record<string, string>>("peer", args);
            const actual = await engine.derivePeerKeys(
              js.key,
              salt,
              purpose,
              room,
              curve,
              initiator,
              responder,
            );
            for (const key of [
              "key",
              "confirmationA",
              "confirmationB",
            ] as const)
              expect(actual[key]).toEqual(decode(expected[key]));
            expect(
              await engine.confirmPeerKey(
                actual.confirmationA,
                actual.confirmationB,
              ),
            ).toBe(false);
            const altered = await engine.derivePeerKeys(
              js.key,
              salt,
              purpose,
              room + "!",
              curve,
              initiator,
              responder,
            );
            expect(altered.key).not.toEqual(actual.key);
            const changed = await engine.derivePeerKeys(
              js.key,
              salt,
              purpose,
              room,
              curve,
              initiator,
              Uint8Array.of(...responder, 32),
            );
            expect(changed.key).not.toEqual(actual.key);
          }
          local.release();
        });
      }
    }
  }
  it.each(["p256", "p384", "p521", "siec", "ed25519"])(
    "matches Go scalar normalization for empty, short and long passwords on %s",
    async (curve) => {
      for (const size of [0, 1, 31, 32, 33, 80]) {
        const password = Uint8Array.from({ length: size }, (_, i) => i % 256);
        const remote = await oracle<Start>("init", {
          Password: password,
          Role: 1,
          Curve: curve,
        });
        const local = new Pake(password, 0, curve);
        const go = await oracle<Finish>("update", {
          Handle: remote.handle,
          Bytes: local.bytes(),
        });
        const js = await local.update(decode(go.bytes));
        expect(js.key).toEqual(decode(go.key));
        local.release();
      }
    },
  );
  it("matches relay PBKDF2 and AES-GCM both ways, rejects tampering and truncation", async () => {
    const salt = Uint8Array.of(0, 1, 2, 3, 4, 5, 6, 7);
    const key = await engine.deriveKey(password, salt);
    expect(key).toEqual(
      decode(await oracle("derive", { Password: password, Salt: salt })),
    );
    const data = new TextEncoder().encode("\nhello 🐊\n世界\u0000");
    const encrypted = await engine.encrypt(data, key);
    expect(
      decode(await oracle("decrypt", { Bytes: encrypted, Key: key })),
    ).toEqual(data);
    expect(
      await engine.decrypt(
        decode(await oracle("encrypt", { Bytes: data, Key: key })),
        key,
      ),
    ).toEqual(data);
    encrypted[13] ^= 1;
    await expect(engine.decrypt(encrypted, key)).rejects.toThrow();
    for (const length of [0, 11, 12, 27])
      await expect(
        engine.decrypt(encrypted.slice(0, length), key),
      ).rejects.toThrow();
  });
  it("matches raw DEFLATE in both directions and bounds decompression", async () => {
    for (const size of [0, 1, 32768, 32776, 100000]) {
      const data = Uint8Array.from({ length: size }, (_, i) => i % 7);
      const encoded = await engine.compress(data);
      expect(
        decode(await oracle("decompress", { Bytes: encoded, Limit: size })),
      ).toEqual(data);
      const go = decode(await oracle("compress", { Bytes: data }));
      expect(await engine.decompress(go, size)).toEqual(data);
      if (size)
        await expect(engine.decompress(go, size - 1)).rejects.toThrow(/limit/);
      await expect(engine.decompress(go.slice(0, -1), size)).rejects.toThrow();
    }
    for (const limit of [-1, NaN, Infinity, 0.5])
      await expect(engine.decompress(new Uint8Array(), limit)).rejects.toThrow(
        /limit/,
      );
    await expect(
      engine.decompress(Uint8Array.of(255, 255, 255), 100),
    ).rejects.toThrow();
  });
  it("matches zero-seed XXH64 across empty, stripe, chunk and fragmented boundaries", async () => {
    for (const size of [
      0, 1, 7, 8, 15, 16, 31, 32, 33, 63, 64, 65, 32768, 32769, 1048576,
    ]) {
      const data = Uint8Array.from(
        { length: size },
        (_, i) => (i * 37 + (i % 31)) % 256,
      );
      const expected = decode(await oracle("hash", { Bytes: data }));
      for (const chunk of [1, 7, 32, 32768, 1048576]) {
        if (size > 32769 && chunk < 32) continue;
        const hash = new XXH64();
        for (let at = 0; at < size; at += chunk)
          hash.update(data.subarray(at, at + chunk));
        expect(hash.digest()).toEqual(expected);
      }
    }
  });
  it("preserves code parsing and hashes the exact UTF-8 code for relay selection", async () => {
    for (const code of [
      "1234-abcd",
      "abc-def-ghi",
      "alpha-beta-gamma-delta",
      "yo-yo-yo-yo-yo-yo",
      "apple-yo-yo-zebra",
      "123456",
      "你好-世界",
      " abc-def-ghi ",
    ]) {
      expect(await codeComponents(code)).toEqual(
        await oracle("code", { Code: code }),
      );
      for (const count of [1, 2, 3, 7, 19])
        expect(await relayIndex(code, count)).toEqual(
          await oracle("relay", { Code: code, Count: count }),
        );
    }
    const code = "apple-yo-yo-zebra-apple-yo-yo-zebra";
    expect(await sshCodeComponents(code)).toEqual(
      await oracle("sshCode", { Code: code }),
    );
  });
  it("rejects wrong passwords through confirmation", async () => {
    const a = new Pake(password, 0, "p256", "peer-transfer", "room");
    const b = new Pake(Uint8Array.of(42), 1, "p256", "peer-transfer", "room");
    const bk = await b.update(a.bytes());
    const ak = await a.update(bk.bytes);
    expect(ak.key).not.toEqual(bk.key);
  });
  it("rejects invalid roles, curves, coordinates, malformed large integers and unknown contexts", async () => {
    expect(() => new Pake(password, 2 as 0, "p256")).toThrow(/role/);
    for (const curve of ["", "toString", "p257"])
      expect(() => new Pake(password, 0, curve)).toThrow(/curve/);
    for (const wire of [
      "null",
      "{}",
      '{"Role":0,"Xᵤ":1e1000,"Xᵥ":0}',
      '{"Role":0,"Xᵤ":-1,"Xᵥ":0}',
      '{"Role":0,"Xᵤ":1,"Xᵥ":1}',
      '{"Role":1,"Xᵤ":1,"Xᵥ":1}',
      `{"Role":0,"Xᵤ":${"1".repeat(1000)},"Xᵥ":0}`,
    ]) {
      await expect(
        new Pake(password, 1, "p256").update(new TextEncoder().encode(wire)),
      ).rejects.toThrow();
    }
    const start = engine.pakeInit(password, 0, "p256");
    engine.pakeRelease(start.handle);
    await expect(
      engine.pakeUpdate(start.handle, new Uint8Array()),
    ).rejects.toThrow(/context/);
    const hash = engine.hashInit();
    engine.hashRelease(hash);
    expect(() => engine.hashFinal(hash)).toThrow(/context/);
    const cipher = await engine.cipherInit(new Uint8Array(32));
    engine.cipherRelease(cipher);
    await expect(
      engine.encodeChunk(cipher, new Uint8Array(), false),
    ).rejects.toThrow(/context/);
  });
});
