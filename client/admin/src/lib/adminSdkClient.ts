import { GramCore } from "@gram/admin-client/core";
import { HTTPClient } from "@gram/admin-client/lib/http";

import { redirectOnUnauthorized, startLoginRedirect } from "@/lib/adminAuth";

// The generated SDK client that reads go through, kept apart from the full SDK
// wrapper so lightweight query modules can build generated queries without
// evaluating every generated operation.
//
// Speakeasy requires an absolute base URL. Browser fetch defaults preserve the
// ambient first-party gram_admin cookie; do not set credentials, mode,
// Authorization, or Cookie here.
let client: GramCore | undefined;

// Created on first use rather than at import, so modules that only build query
// options stay importable outside a browser (tests run some in Node).
export function redirectingClient(): GramCore {
  client ??= new GramCore({
    serverURL: window.location.origin,
    httpClient: new HTTPClient().addHook("response", (response) =>
      startLoginRedirect(response),
    ),
  });
  return client;
}

// Settles an SDK call, sending the browser to login when the session ended.
export async function redirecting<T>(operation: Promise<T>): Promise<T> {
  try {
    return await operation;
  } catch (error) {
    return redirectOnUnauthorized(error);
  }
}
