import type { Backend, Mode } from "@/lib/devidp";

export const MODE_LABELS: Record<Mode, string> = {
  "oauth2-1": "oauth2.1",
  workos: "workos",
};

export const MODE_SUBTITLES: Record<Mode, string> = {
  "oauth2-1":
    "Local identity — the user dev-idp signs you in as, from its own database.",
  workos: "Real WorkOS identity — the live user dev-idp signs you in as.",
};

export const BACKEND_LABELS: Record<Backend, string> = {
  local: "local",
  workos: "workos",
};

export const BACKEND_SUBTITLES: Record<Backend, string> = {
  local: "Fully offline. dev-idp emulates the WorkOS API against its own database.",
  workos: "Passes REST calls through to your real WorkOS environment.",
};

/** Sidebar grouping for the providers sub-nav. Order here is render order. */
export const MODE_GROUPS: ReadonlyArray<{
  title: string;
  modes: ReadonlyArray<Mode>;
}> = [{ title: "Identity", modes: ["oauth2-1", "workos"] }];
