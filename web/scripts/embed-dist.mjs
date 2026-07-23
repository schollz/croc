import { cp, mkdir, rm } from "node:fs/promises";
import path from "node:path";

const webRoot = path.resolve(import.meta.dirname, "..");
const source = path.join(webRoot, "dist");
const destination = path.resolve(webRoot, "..", "src", "webassets", "dist");

await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
await cp(source, destination, { recursive: true });
