export type SkillDetailPage =
  | "overview"
  | "content"
  | "usage"
  | "scored-sessions"
  | "feedback"
  | "versions"
  | "settings";

export function pageFromLegacySkillHash(hash: string): SkillDetailPage | null {
  const value = hash.replace(/^#/, "");
  if (!value) return null;

  switch (value) {
    case "adoption":
    case "timeline":
      return "usage";
    case "insights":
      return "overview";
    case "danger":
      return "settings";
    case "manifest":
    case "frontmatter":
      return "content";
    case "distributions":
      return "usage";
    case "versions":
      return "versions";
    default:
      return "overview";
  }
}

export function pagePath(page: SkillDetailPage): string {
  return page;
}
