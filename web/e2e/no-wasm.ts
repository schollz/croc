import { expect, type BrowserContext } from "@playwright/test";
const forbidden =
  /\/(?:croc(?:-ssh)?\.wasm|croc(?:-ssh)?-worker\.js|wasm_exec\.js)$/;
export async function disableWasm(context: BrowserContext) {
  const requests: string[] = [];
  context.on("request", (request) => {
    if (forbidden.test(new URL(request.url()).pathname))
      requests.push(request.url());
  });
  await context.addInitScript(() =>
    Object.defineProperty(globalThis, "WebAssembly", {
      value: undefined,
      configurable: true,
    }),
  );
  await context.route("**/*", async (route) => {
    const pathname = new URL(route.request().url()).pathname;
    if (forbidden.test(pathname)) return route.abort();
    // Init scripts cover pages. Serve every native worker with the same guard.
    if (/\/(?:engine|sha256)-worker[^/]*\.js$/.test(pathname)) {
      const response = await route.fetch();
      return route.fulfill({
        response,
        body:
          'Object.defineProperty(globalThis,"WebAssembly",{value:undefined,configurable:true});\n' +
          (await response.text()),
      });
    }
    return route.continue();
  });
  return async () => {
    expect(requests, "files/text must never request WASM or Go glue").toEqual(
      [],
    );
    for (const page of context.pages()) {
      if (page.url().startsWith("http"))
        expect(await page.evaluate(() => typeof WebAssembly)).toBe("undefined");
      for (const worker of page.workers()) {
        try {
          expect(await worker.evaluate(() => typeof WebAssembly)).toBe(
            "undefined",
          );
        } catch (error) {
          // Firefox can briefly retain workers that the SHA-256 client disposed.
          if (
            !String(error).includes(
              "Target page, context or browser has been closed",
            )
          )
            throw error;
        }
      }
    }
  };
}
