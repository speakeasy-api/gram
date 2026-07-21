/**
 * Builds the public URL for a skill share token. The path must stay in sync
 * with the hardcoded /shared/skills/:token route registered in App.tsx.
 */
export function skillShareUrl(token: string): string {
  return `${window.location.origin}/shared/skills/${token}`;
}
