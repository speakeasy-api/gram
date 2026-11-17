type BehaviorRule = {
  matches: (urn: string) => boolean;
  bullets: string[];
};

const REACTION_TOOL_URNS = new Set([
  "tools:platform:slack:add_reaction",
  "tools:platform:slack:remove_reaction",
  "tools:platform:slack:get_reactions",
  "tools:platform:slack:list_reactions",
  "tools:platform:slack:list_emoji",
]);

const RULES: BehaviorRule[] = [
  {
    matches: (urn) => urn === "tools:platform:slack:send_message",
    bullets: [
      "Keep the user informed during long-running work by posting status messages.",
      "When referring to a Slack user, look up their profile by ID — never infer a name from the user ID itself.",
    ],
  },
  {
    matches: (urn) => REACTION_TOOL_URNS.has(urn),
    bullets: [
      "Reactions carry meaning — don't decorate, don't spam.",
      "Use reactions as lightweight checkpoints (received, working, done) and as look-back markers in long threads.",
      "Prefer a reaction over a message for well-known statuses (e.g. acknowledging a request you're now processing).",
      "Don't duplicate a reaction with a message saying the same thing.",
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
