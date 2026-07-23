export type DetectedPlatform = "macOS" | "Windows" | "Linux" | "other";
export type DetectedArchitecture = "arm64" | "x64" | "x86" | "unknown";

export interface ReleaseAsset {
  name: string;
  browser_download_url: string;
  size?: number;
}

export interface GitHubRelease {
  tag_name: string;
  html_url: string;
  assets: ReleaseAsset[];
}

interface NavigatorUAData {
  platform?: string;
  getHighEntropyValues?: (
    hints: string[],
  ) => Promise<{ architecture?: string; bitness?: string }>;
}

type NavigatorInfo = Pick<Navigator, "platform" | "userAgent"> & {
  userAgentData?: NavigatorUAData;
};

export const latestReleasePage =
  "https://github.com/schollz/croc/releases/latest";
export const latestReleaseAPI =
  "https://api.github.com/repos/schollz/croc/releases/latest";

export function detectPlatform(
  source: NavigatorInfo = navigator as NavigatorInfo,
): DetectedPlatform {
  const value = [
    source.userAgentData?.platform,
    source.platform,
    source.userAgent,
  ]
    .filter(Boolean)
    .join(" ");
  if (/android|iphone|ipad|ipod/i.test(value)) return "other";
  if (/mac/i.test(value)) return "macOS";
  if (/win/i.test(value)) return "Windows";
  if (/linux|x11/i.test(value)) return "Linux";
  return "other";
}

export async function detectArchitecture(
  source: NavigatorInfo = navigator as NavigatorInfo,
): Promise<DetectedArchitecture> {
  try {
    const values = await source.userAgentData?.getHighEntropyValues?.([
      "architecture",
      "bitness",
    ]);
    const architecture = values?.architecture?.toLowerCase() ?? "";
    if (architecture.includes("arm") && values?.bitness === "64") return "arm64";
    if (/x86|x64|amd64/.test(architecture)) {
      return values?.bitness === "32" ? "x86" : "x64";
    }
  } catch {
    // Fall back to the lower-entropy user agent.
  }
  const value = `${source.platform} ${source.userAgent}`.toLowerCase();
  if (/arm64|aarch64/.test(value)) return "arm64";
  if (/x86_64|x64|win64|amd64/.test(value)) return "x64";
  if (/i[3-6]86|win32/.test(value)) return "x86";
  return "unknown";
}

export function assetsForPlatform(
  assets: ReleaseAsset[],
  platform: DetectedPlatform,
) {
  if (platform === "other") return [];
  const marker = platform === "macOS" ? "_macOS-" : `_${platform}-`;
  return assets.filter((asset) => asset.name.includes(marker));
}

export function assetArchitecture(asset: ReleaseAsset) {
  if (/-ARM64\.(?:tar\.gz|zip)$/i.test(asset.name)) return "arm64";
  if (/-64bit\.(?:tar\.gz|zip)$/i.test(asset.name)) return "x64";
  if (/-32bit\.(?:tar\.gz|zip)$/i.test(asset.name)) return "x86";
  if (/-RISCV64\.(?:tar\.gz|zip)$/i.test(asset.name)) return "RISC-V 64";
  if (/-ARMv5\.(?:tar\.gz|zip)$/i.test(asset.name)) return "ARMv5";
  if (/-ARM\.(?:tar\.gz|zip)$/i.test(asset.name)) return "ARM";
  return "other";
}

export function assetArchitectureLabel(asset: ReleaseAsset) {
  switch (assetArchitecture(asset)) {
    case "arm64":
      return "ARM64";
    case "x64":
      return "64-bit";
    case "x86":
      return "32-bit";
    default:
      return assetArchitecture(asset);
  }
}

export function preferredAsset(
  assets: ReleaseAsset[],
  platform: DetectedPlatform,
  architecture: DetectedArchitecture,
) {
  if (assets.length === 0) return undefined;
  const preferredArchitecture =
    architecture === "unknown"
      ? platform === "macOS"
        ? "arm64"
        : "x64"
      : architecture;
  return (
    assets.find((asset) => assetArchitecture(asset) === preferredArchitecture) ??
    assets[0]
  );
}

export async function fetchLatestRelease(signal?: AbortSignal) {
  const response = await fetch(latestReleaseAPI, {
    signal,
    headers: { Accept: "application/vnd.github+json" },
  });
  if (!response.ok) {
    throw new Error(`GitHub release request failed (${response.status})`);
  }
  const release = (await response.json()) as GitHubRelease;
  if (
    !release.tag_name ||
    !release.html_url ||
    !Array.isArray(release.assets)
  ) {
    throw new Error("GitHub returned invalid release metadata");
  }
  return release;
}
