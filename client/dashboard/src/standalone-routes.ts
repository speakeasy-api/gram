import { SHARED_SKILL_BASE_PATH } from "./pages/skills/share-link";

/**
 * Pages that live outside the organization and project route trees in
 * routes.tsx. App.tsx registers them from here, and the server links to them
 * (src/server-links.json), so moving one breaks a test rather than a link.
 */
export const STANDALONE_ROUTES = {
  riskPolicyBypassRequest: "/risk-policy-bypass/request",
  riskPolicyChallengeAcknowledge: "/risk-policy-challenge/acknowledge",
  riskPolicyChallengeAcknowledgeShort: "/ack",
  block: "/blocks/:id",
  sharedSkill: `${SHARED_SKILL_BASE_PATH}/:token`,
} as const;
