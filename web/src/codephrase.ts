import effWordList from "../../src/codephrase/wordlists/eff-short-wordlist-1.txt?raw";

export const effWords = effWordList.trimEnd().split("\n");

if (effWords.length !== 1296) {
  throw new Error(`EFF word list has ${effWords.length} entries; want 1296`);
}

export function secureRandomIndex(upperBound: number) {
  if (!Number.isSafeInteger(upperBound) || upperBound < 1) {
    throw new RangeError("upperBound must be a positive safe integer");
  }

  const range = 0x1_0000_0000;
  const limit = range - (range % upperBound);
  const value = new Uint32Array(1);
  do {
    crypto.getRandomValues(value);
  } while (value[0] >= limit);
  return value[0] % upperBound;
}

export function generateCode(
  selectIndex: (upperBound: number) => number = secureRandomIndex,
) {
  return Array.from(
    { length: 3 },
    () => effWords[selectIndex(effWords.length)],
  ).join("-");
}

const effWordSet = new Set(effWords);
function effSequence(secret: string, count: number): string[] | undefined {
  const parts = secret.split("-");
  if (parts.length < count || parts.length > count * 2) return;
  const words: string[] = [];
  const visit = (start: number): boolean => {
    if (words.length === count) return start === parts.length;
    for (let end = start + 1; end <= parts.length; end++) {
      const word = parts.slice(start, end).join("-");
      if (!effWordSet.has(word)) continue;
      words.push(word);
      if (visit(end)) return true;
      words.pop();
    }
    return false;
  };
  if (visit(0)) return words;
}
async function digestHex(bytes: Uint8Array) {
  const hash = new Uint8Array(
    await crypto.subtle.digest("SHA-256", bytes.slice()),
  );
  return Array.from(hash, (byte) => byte.toString(16).padStart(2, "0")).join(
    "",
  );
}
export async function codeComponents(secret: string) {
  const encoder = new TextEncoder(),
    bytes = encoder.encode(secret);
  if (bytes.length < 6)
    throw new Error("code is too short (must be at least 6 characters)");
  let selector = bytes.slice(0, 4),
    passphrase = new TextDecoder().decode(bytes.subarray(5));
  let format: "legacy" | "three-word" | "four-word" = "legacy";
  const words =
    effSequence(secret, 3) ??
    (/^[a-z]+-[a-z]+-[a-z]+$/.test(secret) ? secret.split("-") : undefined);
  if (words) {
    selector = encoder.encode(words[0]);
    passphrase = words.slice(1).join("-");
    format = "three-word";
  } else if (/^[a-z]+-[a-z]+-[a-z]+-[a-z]+$/.test(secret)) {
    const words = secret.split("-");
    selector = encoder.encode(words.slice(0, 2).join("-"));
    passphrase = words.slice(2).join("-");
    format = "four-word";
  }
  const roomBytes = new Uint8Array(selector.length + 4);
  roomBytes.set(selector);
  roomBytes.set(encoder.encode("croc"), selector.length);
  return { room: await digestHex(roomBytes), passphrase, format };
}
export async function sshCodeComponents(secret: string) {
  const words = effSequence(secret, 6);
  if (!words) throw new Error("invalid SSH code: expected 6 EFF words");
  return {
    room: await digestHex(
      new TextEncoder().encode(words.slice(0, 2).join("-") + "croc-ssh-v1"),
    ),
    passphrase: words.slice(2).join("-"),
  };
}
export async function relayIndex(secret: string, count: number) {
  if (!Number.isSafeInteger(count) || count <= 0)
    throw new Error("relay count must be positive");
  const hash = await digestHex(new TextEncoder().encode(secret));
  return Number(BigInt("0x" + hash) % BigInt(count));
}
