// Issuer and admission tags share one set of limits: each column caps the list
// at 40 and each entry at 64 characters, and the server refuses anything past
// that. Checked here so the reason lands beside the field.
const MAX_TAGS = 40;
const MAX_TAG_LENGTH = 64;

export function tagsProblem(tags: string[]): string | null {
  if (tags.length > MAX_TAGS) {
    return `At most ${MAX_TAGS} tags.`;
  }
  if (tags.some((tag) => tag.length > MAX_TAG_LENGTH)) {
    return `Each tag is at most ${MAX_TAG_LENGTH} characters.`;
  }
  return null;
}
