// Providers such as Google and Microsoft name scopes as URLs under one shared
// base (https://www.googleapis.com/auth/calendar.readonly), so every scope
// repeats the same 30-odd characters. Showing that base once and each scope by
// what follows it keeps long scope lists readable.

// A base is only worth factoring out when several scopes share it.
const MIN_SHARED = 3;

/**
 * sharedScopePrefix returns the URL base (through its final "/") that the most
 * scopes share, or "" when fewer than three scopes share one.
 */
function sharedScopePrefix(scopes: readonly string[]): string {
  const counts = new Map<string, number>();
  for (const scope of scopes) {
    if (!/^https?:\/\//.test(scope)) continue;
    const base = scope.slice(0, scope.lastIndexOf("/") + 1);
    // A bare origin ("https://") is not a base.
    if (base.length <= "https://".length) continue;
    counts.set(base, (counts.get(base) ?? 0) + 1);
  }
  let best = "";
  let bestCount = 0;
  for (const [base, count] of counts) {
    if (count > bestCount) {
      best = base;
      bestCount = count;
    }
  }
  return bestCount >= MIN_SHARED ? best : "";
}

/** shortScope drops the shared base from a scope that has more after it. */
function shortScope(scope: string, prefix: string): string {
  return prefix && scope.startsWith(prefix) && scope.length > prefix.length
    ? scope.slice(prefix.length)
    : scope;
}

/**
 * scopeLabels maps each scope to its chip label: the scope without the shared
 * base, or the whole scope when that short form is itself another listed scope
 * (a plain "read" beside ".../auth/read"). Short forms can't collide with each
 * other: they all drop the same base.
 */
export function scopeLabels(scopes: readonly string[]): Map<string, string> {
  const prefix = sharedScopePrefix(scopes);
  const listed = new Set(scopes);
  return new Map(
    scopes.map((scope) => {
      const short = shortScope(scope, prefix);
      return [scope, short !== scope && listed.has(short) ? scope : short];
    }),
  );
}
