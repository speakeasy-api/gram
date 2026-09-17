import type { Deployment } from "@gram/client/models/components/deployment.js";
import { describe, expect, it } from "vitest";
import {
  exclusionIdsForSource,
  isTerminalDeploymentStatus,
} from "./removeSourceExclusions";

const active = {
  id: "dep_active",
  openapiv3Assets: [
    { id: "doc_1", assetId: "file_1", slug: "petstore", name: "Petstore" },
    { id: "doc_2", assetId: "file_2", slug: "other", name: "Other" },
  ],
  functionsAssets: [
    { id: "fn_1", assetId: "file_fn_1", slug: "greeter", name: "Greeter" },
  ],
} as unknown as Deployment;

// A newer push re-uploaded petstore under fresh ids and then failed.
const latest = {
  id: "dep_latest",
  openapiv3Assets: [
    { id: "doc_1b", assetId: "file_1b", slug: "petstore", name: "Petstore" },
    { id: "doc_2", assetId: "file_2", slug: "other", name: "Other" },
  ],
  functionsAssets: [],
} as unknown as Deployment;

describe("exclusionIdsForSource", () => {
  it("names the source's ids in every deployment it could be cloned from", () => {
    expect(
      exclusionIdsForSource(
        { kind: "openapi", assetId: "doc_1", slug: "petstore" },
        [active, latest],
      ),
    ).toEqual(["doc_1", "file_1", "doc_1b", "file_1b"]);
  });

  it("matches on the source's kind only", () => {
    expect(
      exclusionIdsForSource(
        { kind: "function", assetId: "fn_1", slug: "greeter" },
        [active, latest],
      ),
    ).toEqual(["fn_1", "file_fn_1"]);
  });

  it("falls back to the caller's id without a slug", () => {
    expect(
      exclusionIdsForSource({ kind: "openapi", assetId: "doc_1" }, [active]),
    ).toEqual(["doc_1"]);
  });

  it("tolerates deployments that have not loaded", () => {
    expect(
      exclusionIdsForSource(
        { kind: "openapi", assetId: "doc_1", slug: "petstore" },
        [undefined, undefined],
      ),
    ).toEqual(["doc_1"]);
  });
});

describe("isTerminalDeploymentStatus", () => {
  it("treats completed and failed as final", () => {
    expect(isTerminalDeploymentStatus("completed")).toBe(true);
    expect(isTerminalDeploymentStatus("failed")).toBe(true);
    expect(isTerminalDeploymentStatus("pending")).toBe(false);
    expect(isTerminalDeploymentStatus("created")).toBe(false);
  });
});
