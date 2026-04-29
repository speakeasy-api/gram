type BehaviorRule = {
  matches: (urn: string) => boolean;
  bullets: string[];
};

const RULES: BehaviorRule[] = [
  {
    matches: (urn) => urn === "tools:platform:slack:send_message",
    bullets: [
      "Keep the user informed during long-running work by posting status messages.",
      "When referring to a Slack user, look up their profile by ID — never infer a name from the user ID itself.",
    ],
  },
];

const DEFAULT_BEHAVIORS: string[] = [
  "IMPORTANT: the user does not see your text responses. You can only communicate by calling tools.",
];

export function computeBehaviorSection(toolUrns: string[]): string {
  const bullets: string[] = [...DEFAULT_BEHAVIORS];
  for (const rule of RULES) {
    if (toolUrns.some((u) => rule.matches(u))) {
      for (const b of rule.bullets) {
        if (!bullets.includes(b)) bullets.push(b);
      }
    }
  }
  return bullets.map((b) => `- ${b}`).join("\n");
}
