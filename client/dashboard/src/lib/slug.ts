// Returns a short random hex token (8 chars, 4 crypto-random bytes) suitable
// for appending to slugs to make them unique. ~4.3 billion values, so
// collisions within a single namespace are negligible.
export function randomSlugSuffix(): string {
  const bytes = new Uint8Array(4);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
