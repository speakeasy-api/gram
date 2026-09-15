import { toASCII } from "tr46";

export function organizationUrlPreview(raw: string): {
  hostname: string;
  error: string;
} {
  const invalid = (error: string) => ({ hostname: "", error });
  if (new TextEncoder().encode(raw).length > 4000) {
    return invalid("Company URL must be at most 4000 bytes.");
  }
  // Check the submitted text before trimming or URL parsing can discard controls.
  // eslint-disable-next-line no-control-regex -- Intentionally reject every C0/DEL character.
  if (/[\\\x00-\x1f\x7f]/.test(raw)) {
    return invalid("Enter a company HTTP(S) URL or hostname.");
  }
  const input = raw.trim();
  if (!input) return { hostname: "", error: "" };
  const full = /^https?:\/\//i.test(input) ? input : `https://${input}`;
  // Inspect before URL normalizes away default ports or Unicode host input.
  const authority = /^https?:\/\/([^/?#]*)/i.exec(full)?.[1];
  if (!authority || /[^a-zA-Z0-9.-]/.test(authority)) {
    return invalid(
      "Use an ASCII hostname without credentials, ports, or IP addresses.",
    );
  }
  const hostname = authority.toLowerCase().replace(/\.$/, "");
  const labels = hostname.split(".");
  if (
    hostname.length > 100 ||
    labels.length < 2 ||
    labels.some(
      (label) => !/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(label),
    ) ||
    hostname.endsWith(".localhost")
  ) {
    return invalid("Use a valid company hostname of at most 100 characters.");
  }
  if (/^(?:[0-9]+|0x[0-9a-f]*)$/.test(labels.at(-1) ?? "")) {
    return invalid("Use a company hostname, not an IP address.");
  }
  // Match idna.Lookup on the server; URL parsing alone uses weaker IDNA checks.
  if (
    toASCII(hostname, {
      checkHyphens: true,
      checkBidi: true,
      checkJoiners: true,
      useSTD3ASCIIRules: true,
      transitionalProcessing: false,
      ignoreInvalidPunycode: false,
    }) !== hostname
  ) {
    return invalid("Use a valid ASCII hostname, including valid punycode.");
  }
  try {
    if (new URL(full).hostname.toLowerCase().replace(/\.$/, "") !== hostname) {
      return invalid("Use an unambiguous company hostname.");
    }
  } catch {
    return invalid("Use a valid ASCII hostname, including valid punycode.");
  }
  return { hostname, error: "" };
}
