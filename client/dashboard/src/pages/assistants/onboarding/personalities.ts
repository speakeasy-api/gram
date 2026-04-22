export type Personality = {
  slug: string;
  title: string;
  summary: string;
  instructions: string;
};

// TODO: pending copy review — full content lands in a follow-up PR.
export const PERSONALITIES: Personality[] = [];

export function getPersonality(slug: string): Personality | undefined {
  return PERSONALITIES.find((p) => p.slug === slug);
}
