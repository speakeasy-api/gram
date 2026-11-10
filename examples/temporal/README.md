# Speakeasy Functions x Temporal Example

This Speakeasy Function shows how to connect a function to a Temporal client for the purposes of querying workflows. This is a starting point, more tools could be added. A temporal client from temporal TS SDK connects to the temporal server over gRPC.

## Usage

- Sign up to [Temporal](https://cloud.temporal.io/) and create a new project
- Create a Namespace that must accept `Allow API Key authentication`
- Retrieve the following environment variables:
  - `TEMPORAL_API_KEY`
  - `TEMPORAL_GRPC_ENDPOINT`
  - `TEMPORAL_NAMESPACE`
- You're all set to build and push this Speakeasy Function!
  - Run `npm install && npm run build && npm run push`.
