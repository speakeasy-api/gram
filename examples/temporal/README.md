# Gram Functions x Temporal Example

This Gram Function shows how to connect to a a gram function to a Temporal client for the purposes of querying workflows. This is a starting point, more tools could be added. A temporal client from temporal TS SDK connects to the temporal server over gRPC.

## Usage

- Sign up to [Temporal](https://cloud.temporal.io/) and create a new project
- Create a Namespace that must accept `Allow API Key authentication`
- Retrieve the following environment variables:
  - `TEMPORAL_API_KEY`
  - `TEMPORAL_GRPC_ENDPOINT`
  - `TEMPORAL_NAMESPACE`
- You're all set to build and push this Gram Function!
  - Run `pnpm install && pnpm build && pnpm push`.

## Notes

Because Temporal's TypeScript SDK has certain dependencies that do not properly bundle to ESM, we have a small custom [postbuild.js](postbuild.js) script to help. This extends our default Gram Functions capabilities to handle these CommonJS imports.
