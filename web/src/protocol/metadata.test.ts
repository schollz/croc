import { describe, expect, it } from "vitest";
import { normalizeOutgoingFileName, validateSenderInfo } from "./metadata";
import type { SenderInfoWire } from "./types";

function sender(
  files: SenderInfoWire["FilesToTransfer"],
  folders: SenderInfoWire["EmptyFoldersToTransfer"] = null,
): SenderInfoWire {
  return {
    FilesToTransfer: files,
    EmptyFoldersToTransfer: folders,
    TotalNumberFolders: folders?.length ?? 0,
    MachineID: "sender",
    Ask: false,
    SendingText: false,
    NoCompress: false,
    HashAlgorithm: "xxhash",
  };
}

describe("incoming croc metadata", () => {
  it("normalizes Unicode separators in outgoing filenames for Go compatibility", () => {
    expect(
      normalizeOutgoingFileName("Screenshot 2026-07-22 at 8.59.57\u202fAM.png"),
    ).toBe("Screenshot 2026-07-22 at 8.59.57 AM.png");
    expect(normalizeOutgoingFileName("two\u00a0spaces.txt")).toBe("two spaces.txt");
    expect(() => normalizeOutgoingFileName("hidden\u200bmark.txt")).toThrow(
      /non-printable/i,
    );
  });

  it("normalizes safe nested paths", () => {
    const offer = validateSenderInfo(
      sender([
        {
          n: "hello.txt",
          fr: "docs/notes/",
          s: 5,
          h: "AQID",
        },
      ], [{ fr: "empty/folder/" }]),
    );
    expect(offer.files[0]).toMatchObject({
      name: "hello.txt",
      folder: "docs/notes",
      path: "docs/notes/hello.txt",
      size: 5,
    });
    expect(offer.emptyFolders).toEqual(["empty/folder"]);
  });

  it.each([
    ["../escape", "file.txt"],
    ["/absolute", "file.txt"],
    [".ssh", "authorized_keys"],
    [".", "../file.txt"],
  ])("rejects unsafe path %s/%s", (folder, name) => {
    expect(() =>
      validateSenderInfo(sender([{ n: name, fr: folder, s: 1, h: "AA==" }])),
    ).toThrow();
  });

  it("rejects duplicate destinations", () => {
    expect(() =>
      validateSenderInfo(
        sender([
          { n: "same.txt", fr: "./", s: 1, h: "AA==" },
          { n: "same.txt", fr: ".", s: 1, h: "AA==" },
        ]),
      ),
    ).toThrow(/duplicate/i);
  });

  it("rejects symlinks, text mode, unsupported hashes, and unsafe sizes", () => {
    expect(() =>
      validateSenderInfo(sender([{ n: "link", sy: "../target", s: 0 }])),
    ).toThrow(/symlink/i);

    const text = sender([]);
    text.SendingText = true;
    expect(() => validateSenderInfo(text)).toThrow(/text transfers/i);

    const md5 = sender([]);
    md5.HashAlgorithm = "md5";
    expect(() => validateSenderInfo(md5)).toThrow(/md5/i);

    expect(() =>
      validateSenderInfo(
        sender([{ n: "huge", fr: ".", s: Number.MAX_VALUE, h: "AA==" }]),
      ),
    ).toThrow(/size/i);
  });
});
