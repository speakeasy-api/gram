/**
 * Quotes a value for a POSIX shell so it survives copy-paste as one
 * argument. Single quotes suppress every expansion; the only character that
 * needs care inside them is the single quote itself, which is closed,
 * escaped, and reopened.
 */
export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}
