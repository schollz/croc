import { cp, mkdir, rm, writeFile } from "node:fs/promises";
import path from "node:path";

const webRoot = path.resolve(import.meta.dirname, "..");
const source = path.join(webRoot, "dist");
const destination = path.resolve(webRoot, "..", "src", "webassets", "dist");
const installer = path.resolve(
  webRoot,
  "..",
  "src",
  "install",
  "default.txt",
);

await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
await cp(source, destination, { recursive: true });
await cp(installer, path.join(destination, "default.txt"));
await writeFile(path.join(destination, ".gitkeep"), "");
