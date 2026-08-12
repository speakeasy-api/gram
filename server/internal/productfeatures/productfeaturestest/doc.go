// Package productfeaturestest provides helpers for toggling organization-level
// product feature entitlements in tests, for the packages whose service is gated
// on one.
//
// Toggling an entitlement is a two-step write — the organization_features row
// and then the Redis cache the client reads first — and getting only half of it
// right fails quietly: the row lands but a stale cache keeps answering the old
// value, or the cache flips but the row survives to contradict it on the next
// eviction. Centralizing the pair keeps every caller doing both.
package productfeaturestest
