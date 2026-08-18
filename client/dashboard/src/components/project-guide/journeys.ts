export type JourneyId = "third-party-mcp" | "secret-block";

export type JourneyStatus = "not-started" | "in-progress" | "done";

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
  steps: string[];
};

export const PROJECT_GUIDE_JOURNEYS: JourneyMeta[] = [
  {
    id: "third-party-mcp",
    index: "01",
    title: "Govern third-party MCP usage",
    win: "Your agent reaches a third-party MCP through Speakeasy, as a governed endpoint.",
    steps: ["Pick a server", "Deploy it", "Connect and verify"],
  },
  {
    id: "secret-block",
    index: "02",
    title: "Catch a secret before it leaves",
    win: "Watch a prompt carrying a secret get blocked.",
    steps: [
      "Turn on secret detection",
      "Install the observability plugin",
      "Trigger it",
    ],
  },
];
