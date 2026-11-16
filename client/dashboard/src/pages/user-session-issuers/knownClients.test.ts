// @vitest-environment node
import { expect, it } from "vitest";
import { groupKnownClients, monogram, summarizeDocument } from "./knownClients";

const preset = (displayName: string, url: string, enabled = true) => ({
  displayName,
  clientIdMetadataUri: url,
  enabled,
  isPattern: url.includes("*"),
  vendorKey: "vendor",
});

it("groups a client's documents under the name people know it by, leaving out disabled ones", () => {
  const known = groupKnownClients([
    preset(
      "ChatGPT (connectors)",
      "https://chatgpt.example/oauth/*/client.json",
    ),
    preset("Zed", "https://zed.example/client.json"),
    preset("ChatGPT", "https://chatgpt.example/oauth/client.json"),
    preset("Retired", "https://retired.example/client.json", false),
  ]);
  expect(known.map((client) => client.name)).toEqual(["ChatGPT", "Zed"]);
  expect(known[0]?.documents).toEqual([
    {
      name: "ChatGPT (connectors)",
      url: "https://chatgpt.example/oauth/*/client.json",
      isPattern: true,
    },
    {
      name: "ChatGPT",
      url: "https://chatgpt.example/oauth/client.json",
      isPattern: false,
    },
  ]);
});

it("makes a short monogram for a client without a logo", () => {
  expect(monogram("Factory Droid")).toBe("FD");
  expect(monogram("Zed")).toBe("Ze");
});

it("reads callback URLs and website from a client document, and nothing from a broken one", () => {
  expect(
    summarizeDocument(
      JSON.stringify({
        redirect_uris: ["https://a.example/cb", 7],
        client_uri: "https://a.example",
      }),
    ),
  ).toEqual({
    callbackUrls: ["https://a.example/cb"],
    website: "https://a.example",
  });
  expect(summarizeDocument("{not json")).toEqual({
    callbackUrls: [],
    website: null,
  });
  expect(summarizeDocument(undefined)).toEqual({
    callbackUrls: [],
    website: null,
  });
});
