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
  return Array.from({ length: 3 }, () => effWords[selectIndex(effWords.length)]).join(
    "-",
  );
}
