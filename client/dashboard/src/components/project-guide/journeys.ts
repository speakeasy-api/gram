export type JourneyId = "third-party-mcp" | "secret-block";

export type JourneyStatus = "not-started" | "in-progress" | "done";

export function otherProjectGuideJourney(id: JourneyId): JourneyId {
  return id === "third-party-mcp" ? "secret-block" : "third-party-mcp";
}

/** Catalog entries we surface first in the automatic project-guide chooser. */
export const AUTOMATIC_CATALOG_SERVER_NAMES = [
  "Linear",
  "Notion",
  "Vercel",
  "Granola",
  "Ramp",
] as const;

export const JOURNEY_STATUS_LABELS: Record<JourneyStatus, string> = {
  "not-started": "Not started",
  "in-progress": "In progress",
  done: "Done",
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
};

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
    win: "Watch a synthetic credential get blocked before it reaches the model.",
    completion: {
      eyebrow: "Journey B complete",
      heading: "The prompt was denied.",
      body: "The synthetic credential was denied before it reached the model.",
      primaryAction: "Open Risk Events",
    },
    steps: [
      "Turn on secret detection",
      "Install the observability plugin",
      "Trigger it",
    ],
  },
  {
    id: "third-party-mcp",
    index: "02",
    title: "Govern a third-party MCP",
    win: "Your agent reaches a third-party MCP through Speakeasy, as a governed endpoint.",
    completion: {
      eyebrow: "Journey A complete",
      heading: "The path is governed.",
      body: "Your client traffic is governed and recorded in Tool Logs.",
      primaryAction: "Open Tool Logs",
    },
    steps: ["Pick a server", "Deploy it", "Connect and verify"],
  },
];
