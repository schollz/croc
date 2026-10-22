import { ProtocolOperations } from "./operations";
const operations = new ProtocolOperations();
const methods = new Set(
  Object.getOwnPropertyNames(ProtocolOperations.prototype).concat(
    Object.keys(operations),
  ),
);
function transfers(value: unknown, output: ArrayBuffer[] = []): ArrayBuffer[] {
  if (value instanceof Uint8Array) output.push(value.buffer as ArrayBuffer);
  else if (value && typeof value === "object")
    for (const child of Object.values(value)) transfers(child, output);
  return output;
}
// Web Crypto and streams are asynchronous: serialize calls to preserve context order.
let tail = Promise.resolve();
self.addEventListener("message", (event) => {
  const { id, method, args } = event.data;
  tail = tail.then(async () => {
    try {
      if (
        !methods.has(method) ||
        ["constructor", "get", "start"].includes(method)
      )
        throw new Error("Unknown protocol method");
      const fn = operations[method as keyof ProtocolOperations] as (
        ...args: unknown[]
      ) => unknown;
      if (typeof fn !== "function") throw new Error("Unknown protocol method");
      const result = await fn.apply(operations, args);
      self.postMessage({ id, result }, { transfer: transfers(result) });
    } catch (error) {
      self.postMessage({
        id,
        error:
          (error instanceof Error
            ? error.message || error.name
            : String(error)) || "Protocol operation failed",
      });
    }
  });
});
