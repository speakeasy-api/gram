// Pure (non-component) helpers for displaying AI accounts, kept separate from
// account-display.tsx so that file only exports components (the react-refresh
// "only-export-components" rule) and these can be shared with non-component
// modules like the cost taxonomy.

// A single linked AI account in display shape. Identity is (provider, email):
// the same email on two providers is two distinct accounts, so provider is
// always shown alongside the email.
export type DisplayAccount = {
  email: string;
  provider: string;
  // "team" | "personal" | "" (unclassified).
  accountType: string;
};

// Friendly labels for the AI providers an account can belong to. Falls back to a
// capitalized provider slug so a newly-added provider still renders sensibly.
const PROVIDER_LABELS: Record<string, string> = {
  anthropic: "Anthropic",
  openai: "OpenAI",
  cursor: "Cursor",
  google: "Google",
  aws: "AWS",
};

export function providerLabel(provider: string): string {
  if (!provider) return "Unknown";
  const known = PROVIDER_LABELS[provider.toLowerCase()];
  if (known) return known;
  return provider.charAt(0).toUpperCase() + provider.slice(1);
}

// The email to display for a session produced by a personal AI account (e.g. a
// gmail on Claude Max), or undefined for team/unclassified sessions so callers
// fall back to the employee identity. Personal sessions are attributed to the
// employee via the device bridge, so the session's user fields carry the WORK
// email — this surfaces the account actually used.
export function personalAccountEmail(chat: {
  accountType?: string | undefined;
  accountEmail?: string | undefined;
}): string | undefined {
  if (chat.accountType === "personal" && chat.accountEmail) {
    return chat.accountEmail;
  }
  return undefined;
}
