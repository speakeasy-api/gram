import { describe, expect, it, vi } from "vitest";
import {
  ProxyRegistrationError,
  proxyRegisterUpstreamClient,
  type AuthedFetch,
} from "@/lib/proxyRegisterUpstreamClient";

function jsonResponse(body: unknown, init?: ResponseInit): Response {
  return new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
}

describe("proxyRegisterUpstreamClient", () => {
  it("returns the registered client on success", async () => {
    const authedFetch: AuthedFetch = vi.fn(async () =>
      jsonResponse({
        client_id: "abc",
        client_secret: "shh",
        token_endpoint_auth_method: "client_secret_basic",
      }),
    );

    const result = await proxyRegisterUpstreamClient(authedFetch, {
      registrationEndpoint: "https://idp.example/register",
    });

    expect(result).toEqual({
      clientId: "abc",
      clientSecret: "shh",
      tokenEndpointAuthMethod: "client_secret_basic",
    });
  });

  it("forwards the issuer tunnel binding for registration", async () => {
    const authedFetch: AuthedFetch = vi.fn(async () =>
      jsonResponse({ client_id: "abc" }),
    );

    await proxyRegisterUpstreamClient(authedFetch, {
      registrationEndpoint: "https://idp.example/register",
      tunneledMcpServerId: "019c001e-b43d-7000-8000-000000000001",
      projectSlug: "private-network",
    });

    const call = vi.mocked(authedFetch).mock.calls[0];
    expect(call).toBeDefined();
    const [, init] = call!;
    if (typeof init.body !== "string") {
      throw new Error("expected a JSON request body");
    }
    expect(JSON.parse(init.body)).toMatchObject({
      tunneled_mcp_server_id: "019c001e-b43d-7000-8000-000000000001",
    });
    expect(init.headers).toMatchObject({
      "gram-project": "private-network",
    });
  });

  it("surfaces the passed-through upstream message on a 4xx", async () => {
    const authedFetch: AuthedFetch = vi.fn(async () =>
      jsonResponse(
        {
          Message:
            "identity provider rejected the client registration: invalid_client_metadata: redirect_uris must be loopback",
          Name: "bad_request",
        },
        { status: 400 },
      ),
    );

    const result = proxyRegisterUpstreamClient(authedFetch, {
      registrationEndpoint: "https://idp.example/register",
    });

    await expect(result).rejects.toBeInstanceOf(ProxyRegistrationError);
    await expect(result).rejects.toMatchObject({
      title: "Registration failed (HTTP 400)",
      message:
        "identity provider rejected the client registration: invalid_client_metadata: redirect_uris must be loopback",
    });
  });

  it("falls back to error_description when no message field is present", async () => {
    const authedFetch: AuthedFetch = vi.fn(async () =>
      jsonResponse(
        { error: "invalid_scope", error_description: "scope not supported" },
        { status: 400 },
      ),
    );

    await expect(
      proxyRegisterUpstreamClient(authedFetch, {
        registrationEndpoint: "https://idp.example/register",
      }),
    ).rejects.toThrow("scope not supported");
  });

  it("preserves the status fallback when no details are returned", async () => {
    const authedFetch: AuthedFetch = vi.fn(
      async () => new Response("gateway boom", { status: 502 }),
    );

    const result = proxyRegisterUpstreamClient(authedFetch, {
      registrationEndpoint: "https://idp.example/register",
    });

    await expect(result).rejects.toMatchObject({
      title: "Registration failed (HTTP 502)",
      message: "Registration failed (HTTP 502)",
    });
  });
});
