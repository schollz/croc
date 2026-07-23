import { describe, expect, it } from "vitest";
import {
  assetArchitectureLabel,
  assetsForPlatform,
  detectArchitecture,
  detectPlatform,
  preferredAsset,
  type ReleaseAsset,
} from "./releases";

const assets: ReleaseAsset[] = [
  {
    name: "croc_v10.5.0_macOS-64bit.tar.gz",
    browser_download_url: "https://example.test/mac-intel",
  },
  {
    name: "croc_v10.5.0_macOS-ARM64.tar.gz",
    browser_download_url: "https://example.test/mac-arm",
  },
  {
    name: "croc_v10.5.0_Windows-64bit.zip",
    browser_download_url: "https://example.test/windows",
  },
];

describe("release downloads", () => {
  it.each([
    ["MacIntel", "Mozilla/5.0 (Macintosh)", "macOS"],
    ["Win32", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)", "Windows"],
    ["Linux x86_64", "Mozilla/5.0 (X11; Linux x86_64)", "Linux"],
    ["Linux armv8l", "Mozilla/5.0 (Linux; Android 15)", "other"],
  ])("detects %s as %s", (platform, userAgent, expected) => {
    expect(detectPlatform({ platform, userAgent })).toBe(expected);
  });

  it("uses high-entropy architecture data when available", async () => {
    await expect(
      detectArchitecture({
        platform: "MacIntel",
        userAgent: "Mozilla/5.0 (Macintosh)",
        userAgentData: {
          getHighEntropyValues: async () => ({
            architecture: "arm",
            bitness: "64",
          }),
        },
      }),
    ).resolves.toBe("arm64");
  });

  it("filters and prioritizes matching release assets", () => {
    const macAssets = assetsForPlatform(assets, "macOS");
    expect(macAssets).toHaveLength(2);
    expect(preferredAsset(macAssets, "macOS", "arm64")?.browser_download_url).toBe(
      "https://example.test/mac-arm",
    );
    expect(assetArchitectureLabel(macAssets[0])).toBe("64-bit");
  });
});
