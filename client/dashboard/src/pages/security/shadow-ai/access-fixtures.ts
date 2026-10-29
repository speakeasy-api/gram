import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import type { AiScanTarget } from "@gram/client/models/components/aiscantarget.js";

/** Test fixtures for the Access step: a library of three kinds, one of them
 *  seen by the device agent. */
export function scanTarget(
  overrides: Partial<AiScanTarget> = {},
): AiScanTarget {
  return {
    id: "cursor",
    displayName: "Cursor",
    category: "harness",
    signatures: {
      bundleIds: [],
      binaries: ["cursor"],
      configDirs: [],
      processNames: [],
    },
    gatewayClient: {
      cimdVendorKeys: ["cursor"],
      oauthClientIds: [],
      clientInfoNames: [],
    },
    origin: "default",
    customized: false,
    access: { state: "unreviewed", decision: "unreviewed", enforceable: true },
    ...overrides,
  };
}

export function accessLibrary(): AiScanTarget[] {
  return [
    scanTarget(),
    scanTarget({
      id: "acme-tool",
      displayName: "Acme Tool",
      category: "assistant",
      origin: "organization",
    }),
    scanTarget({
      id: "ollama",
      displayName: "Ollama",
      category: "local_model",
      gatewayClient: {
        cimdVendorKeys: [],
        oauthClientIds: [],
        clientInfoNames: [],
      },
      access: {
        state: "unreviewed",
        decision: "unreviewed",
        enforceable: false,
      },
    }),
  ];
}

export function detection(overrides: Partial<AIDetection> = {}): AIDetection {
  return {
    targetId: "cursor",
    displayName: "Cursor",
    category: "harness",
    userCount: 4,
    deviceCount: 6,
    signals: ["installed", "running"],
    versions: [],
    firstSeen: new Date("2026-09-01T00:00:00Z"),
    lastSeen: new Date("2026-09-10T00:00:00Z"),
    access: { state: "unreviewed", decision: "unreviewed", enforceable: true },
    ...overrides,
  };
}
