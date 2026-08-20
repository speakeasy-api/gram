export type JourneyId = "third-party-mcp" | "secret-block";

export type JourneyStatus =
  | "not-started"
  | "in-progress"
  | "done"
  | "unreadable";

export function otherProjectGuideJourney(id: JourneyId): JourneyId {
  return id === "third-party-mcp" ? "secret-block" : "third-party-mcp";
}

/** Catalog entries we surface first in the automatic project-guide chooser. */
export const AUTOMATIC_CATALOG_SERVER_NAMES = [
  "GitHub",
  "Notion",
  "Stripe",
  "Figma",
  "Linear",
  "Cloudflare",
] as const;

export const JOURNEY_STATUS_LABELS: Record<JourneyStatus, string> = {
  "not-started": "Not started",
  "in-progress": "In progress",
  done: "Done",
  unreadable: "Progress unavailable",
};

export type JourneyMeta = {
  id: JourneyId;
  /** Matches the numbering language of the org-home welcome banner cards. */
  index: string;
  title: string;
  /** The observable win, stated as the user would describe it afterwards. */
  win: string;
  completion: {
    eyebrow: string;
    heading: string;
    body: string;
    primaryAction: string;
  };
  steps: string[];
  stepBlurbs: string[];
};

export type JourneyFixture = {
  meta: string;
  accent: string;
  activity: string;
  event: {
    label: string;
    kind: string;
    title: string;
    rows: Array<{ key: string; value: string }>;
    note: string;
    tone: "allow" | "deny";
  };
};

export const SECRET_BLOCK_STEPS = [
  "Create a secrets policy set to deny",
  "Download the observability plugin",
  "Add it to your agent",
  "Send a prompt with a synthetic secret",
  "Watch the block land",
];

export const THIRD_PARTY_MCP_STEPS = [
  "Pick a server from the catalog",
  "Confirm the governed endpoint",
  "Connect your client",
  "Ask the agent to list the tools",
  "Watch the first governed call",
];

export const PROJECT_GUIDE_COMPLETE = {
  eyebrow: "Both journeys complete",
  heading: "Both journeys are on the record.",
  body: "Traffic is proxied and recorded, and prompts are inspected before transport. Project home now shows live calls, policies, and risk events instead of this card.",
  primaryAction: "Go to project home",
  secondaryAction: "Review what you set up",
  note: "This card is replaced on next visit",
} as const;

export const PROJECT_GUIDE_JOURNEYS: JourneyMeta[] = [
  {
    id: "secret-block",
    index: "01",
    title: "Block a leaked credential mid-prompt",
    win: "A secrets policy denies any prompt carrying a credential. The attempt lands in Risk Events with the rule that caught it, the matched span, and who sent it.",
    completion: {
      eyebrow: "Journey B complete",
      heading: "The prompt was denied.",
      body: "The prompt matched the secrets policy and was rejected before the model answered. The finding sits in Risk Events with the rule that fired, the matched span, the severity, and who sent it.",
      primaryAction: "Open Risk Events",
    },
    steps: SECRET_BLOCK_STEPS,
    stepBlurbs: [
      "A policy goes live that looks for secrets — API keys, tokens, private keys — in the prompts people send. It denies anything that matches, for everyone in the org.",
      "Speakeasy builds the observability plugin for this project and signs it. You get a package to download — the next step installs it in your client.",
      "Run the command below to install the observability plugin, then restart your agent so its activity can stream into this project.",
      "This key is synthetic and inert. It exists so the rule has something real-shaped to catch. The call should not survive the machine.",
      "The policy denies the request and a finding lands in Risk Events. It carries the rule that fired, the matched span, the severity, and who sent it.",
    ],
  },
  {
    id: "third-party-mcp",
    index: "02",
    title: "Govern a third-party MCP",
    win: "Install a vendor's MCP server, connect your agent to it, and watch the first call arrive with the actor, the tools, and the result attached.",
    completion: {
      eyebrow: "Journey A complete",
      heading: "The path is governed.",
      body: "Your client now reaches the selected server through an endpoint you own. Tool lists are filtered to what each caller may use, every call lands in tool logs, and the vendor's server never changed. Remove the server and the path closes.",
      primaryAction: "Open tool logs",
    },
    steps: THIRD_PARTY_MCP_STEPS,
    stepBlurbs: [
      "The catalog lists servers from the official MCP Registry. Installing one creates a governed endpoint in front of the vendor's server — the vendor's URL is already known, and nothing upstream changes.",
      "Installing created your endpoint. Confirming it proves the upstream is reachable and speaking MCP, and shows what the endpoint already covers.",
      "Point the client at your endpoint instead of the vendor's URL. The proxy drops the client's own Authorization header and substitutes the credential resolved for that caller, so no vendor key sits on a developer's machine.",
      "Run this in the client you just configured. Listing tools reads nothing and writes nothing — it puts the first real request on the governed path.",
      "The endpoint checks the call against the caller's tool access and records it before forwarding. This is the first entry.",
    ],
  },
];

export const PROJECT_GUIDE_FIXTURES: Record<JourneyId, JourneyFixture> = {
  "third-party-mcp": {
    meta: "govern a third-party MCP",
    accent: "#2879D8",
    activity: "Endpoint verified, client connected, no calls recorded.",
    event: {
      label: "The call you watched",
      kind: "Governed call",
      title: "linear.tools/list",
      rows: [
        { key: "access", value: "allowed · 27 of 27 tools visible" },
        { key: "upstream", value: "forwarded with the resolved credential" },
      ],
      note: "The call landed in Tool Logs before it reached the upstream.",
      tone: "allow",
    },
  },
  "secret-block": {
    meta: "block a leaked credential",
    accent: "#B45A28",
    activity: "Policy enforcing, one agent streaming, no findings yet.",
    event: {
      label: "The event you watched",
      kind: "Denied · risk event",
      title: "request denied by secrets policy",
      rows: [
        { key: "rule", value: "secrets.aws_access_key_id" },
        { key: "match", value: "AKIA···· · highlighted in the transcript" },
      ],
      note: "The request was blocked before the model answered.",
      tone: "deny",
    },
  },
};
