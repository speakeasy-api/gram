// Escape hatch type — flags any-cast call sites with a required reason message
// so they're greppable and reviewable.
//
// Usage:
//   const value = apiCall() as FIXME<"Need to fix upstream API types">;

// oxlint-disable-next-line typescript/no-explicit-any
type FIXME<M extends string> = any;
