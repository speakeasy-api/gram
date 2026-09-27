const compactFormatter = new Intl.NumberFormat("en", {
  notation: "compact",
  maximumFractionDigits: 1,
});

export function formatCompact(value: number): string {
  return compactFormatter.format(value);
}

/** Regular English plurals only; pass irregular nouns already pluralized elsewhere. */
export function pluralizeNoun(noun: string): string {
  if (/[^aeiou]y$/.test(noun)) return `${noun.slice(0, -1)}ies`;
  if (/(s|x|z|ch|sh)$/.test(noun)) return `${noun}es`;
  return `${noun}s`;
}

export function pluralize(count: number, noun: string): string {
  return `${count} ${count === 1 ? noun : pluralizeNoun(noun)}`;
}
