// Package dns provides a [Resolver] interface for DNS lookups whose method
// signatures match [net.Resolver]. Use [NewNetResolver] for production and
// [NewMockResolver] for deterministic testing. [MockResolver.Resolver] returns
// a [net.Resolver] with a custom Dial that routes queries through the mock
// functions, so it can be used anywhere a [net.Resolver] is expected.
package dns
