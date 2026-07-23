import { rm } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export default async function globalTeardown() {
  const binaryName = process.env.CROC_E2E_BINARY_NAME;
  if (!binaryName) return;
  const webDirectory = dirname(dirname(fileURLToPath(import.meta.url)));
  await rm(join(webDirectory, ".e2e", binaryName), { force: true }).catch(
    () => undefined,
  );
}
