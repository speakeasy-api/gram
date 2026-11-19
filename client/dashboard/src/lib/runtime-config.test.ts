import fs from "node:fs";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  DASHBOARD_CONFIG_META_NAME,
  DASHBOARD_CONFIG_PLACEHOLDER,
  replaceDashboardConfig,
} from "./dashboard-config-html";
import {
  datadogRumConfig,
  getRuntimeConfig,
  parseRuntimeConfig,
  posthogConfig,
} from "./runtime-config";

const full = {
  posthogKey: "phc_test",
  posthogHost: "https://metrics.example.com",
  datadogRumApplicationId: "app",
  datadogRumClientToken: "pubtoken",
  datadogRumSite: "datadoghq.com",
  datadogRumEnv: "prod",
  tunnelHost: "tunnel.example.com",
  customDomainCname: "cname.example.com.",
  pylonAppId: "pylon",
  firstPartyHosts: ["app.example.com", "ai.example.com"],
};

afterEach(() => {
  document
    .querySelectorAll(`meta[name="${DASHBOARD_CONFIG_META_NAME}"]`)
    .forEach((meta) => meta.remove());
  vi.restoreAllMocks();
});

describe("parseRuntimeConfig", () => {
  it("reads every setting", () => {
    expect(parseRuntimeConfig(JSON.stringify(full))).toEqual(full);
  });

  it.each([
    ["the empty image default", ""],
    ["a missing tag", null],
    ["the unsubstituted placeholder", DASHBOARD_CONFIG_PLACEHOLDER],
    ["a JSON array", "[]"],
    ["JSON null", "null"],
  ])("treats %s as an empty configuration", (_name, raw) => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(parseRuntimeConfig(raw)).toEqual({});
    expect(error).not.toHaveBeenCalled();
  });

  it("reports malformed JSON and treats it as empty", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    expect(parseRuntimeConfig("{not json")).toEqual({});
    expect(error).toHaveBeenCalledOnce();
  });

  it("drops only the invalid fields", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          ...full,
          posthogHost: "http://metrics.example.com",
          tunnelHost: "wss://tunnel.example.com/connect",
          datadogRumSite: 42,
          firstPartyHosts: [],
        }),
      ),
    ).toEqual({
      ...full,
      posthogHost: undefined,
      tunnelHost: undefined,
      datadogRumSite: undefined,
      firstPartyHosts: undefined,
    });
  });

  it("lowercases hosts so they compare with URL.hostname", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          tunnelHost: "Tunnel.Example.com",
          firstPartyHosts: ["App.Example.com"],
        }),
      ),
    ).toEqual({
      tunnelHost: "tunnel.example.com",
      firstPartyHosts: ["app.example.com"],
    });
  });

  it("allows a port on the tunnel host but not on first-party hosts", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({
          tunnelHost: "localhost:8443",
          firstPartyHosts: ["app.example.com:8443"],
        }),
      ),
    ).toEqual({ tunnelHost: "localhost:8443" });
  });

  it("ignores settings this image does not know about", () => {
    expect(
      parseRuntimeConfig(
        JSON.stringify({ tunnelHost: "t.example.com", later: 1 }),
      ),
    ).toEqual({ tunnelHost: "t.example.com" });
  });
});

describe("getRuntimeConfig", () => {
  it("reads the meta tag the entrypoint fills", () => {
    const meta = document.createElement("meta");
    meta.name = DASHBOARD_CONFIG_META_NAME;
    meta.content = JSON.stringify({ tunnelHost: "tunnel.example.com" });
    document.head.appendChild(meta);
    expect(getRuntimeConfig()).toEqual({ tunnelHost: "tunnel.example.com" });

    meta.content = JSON.stringify({ tunnelHost: "other.example.com" });
    expect(getRuntimeConfig()).toEqual({ tunnelHost: "other.example.com" });
  });

  it("is empty without the meta tag", () => {
    expect(getRuntimeConfig()).toEqual({});
  });
});

describe("posthogConfig", () => {
  it("needs both the key and the host", () => {
    expect(posthogConfig(full)).toEqual({
      key: "phc_test",
      apiHost: "https://metrics.example.com",
    });
    expect(posthogConfig({ posthogKey: "phc_test" })).toBeNull();
    expect(
      posthogConfig({ posthogHost: "https://metrics.example.com" }),
    ).toBeNull();
  });
});

describe("datadogRumConfig", () => {
  it("needs every RUM setting", () => {
    expect(datadogRumConfig(full)).toEqual({
      applicationId: "app",
      clientToken: "pubtoken",
      site: "datadoghq.com",
      env: "prod",
    });
    expect(datadogRumConfig({ ...full, datadogRumEnv: undefined })).toBeNull();
  });
});

describe("index.html placeholder", () => {
  const indexHtml = fs.readFileSync("index.html", "utf8");

  it("is left in index.html for the entrypoint", () => {
    expect(indexHtml).toContain(
      `<meta name="${DASHBOARD_CONFIG_META_NAME}" content="${DASHBOARD_CONFIG_PLACEHOLDER}" />`,
    );
  });

  it("round-trips JSON through the attribute escaping", () => {
    const value = JSON.stringify({ ...full, pylonAppId: `a"<b>&c` });
    const html = replaceDashboardConfig(indexHtml, value);
    const doc = new DOMParser().parseFromString(html, "text/html");
    const content = doc
      .querySelector(`meta[name="${DASHBOARD_CONFIG_META_NAME}"]`)
      ?.getAttribute("content");
    expect(content).toBe(value);
  });
});
