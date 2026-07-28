export function formatGeneratedCode(code: string, compact: boolean) {
  if (!compact) return code;

  const segments = code.split("-");
  return segments.length > 3 ? segments.slice(0, 3).join("-") : code;
}
