import { createHash, randomBytes } from "node:crypto";
import { test, expect, rpc, runId } from "../fixtures.js";

type McpServer = { name: string; slug: string; primary_endpoint_url?: string };

// Never contacted: the browser's redirect to it is intercepted to read the code.
const redirectURI = "http://127.0.0.1:47999/cb";

test("add an MCP server from the catalog and connect to it", async ({
  page,
  project,
}) => {
  const name = `Flow catalog ${runId()}`;
  let endpointURL = "";
  let serverPath = "";

  await test.step("Open the MCP catalog", async () => {
    await page.goto(`/${project.orgSlug}/projects/${project.projectSlug}/mcp`);
    await page.getByRole("tab", { name: "Catalog" }).click();
    await expect(page).toHaveURL(/\/mcp\/catalog$/);
    await expect(
      page.getByRole("heading", { name: "MCP Catalog" }),
    ).toBeVisible();
  });

  await test.step('Search for "DeepWiki"', async () => {
    await page.getByPlaceholder("Search MCP servers...").fill("DeepWiki");
    await expect(
      page.getByRole("link", { name: /^DeepWiki (Added )?DeepWiki/ }),
    ).toBeVisible();
  });

  await test.step(`Add it to the project, named "Flow catalog <runId>"`, async () => {
    await page
      .getByRole("link", { name: /^DeepWiki (Added )?DeepWiki/ })
      .getByRole("button", { name: /^Add( again)?$/ })
      .click();
    const dialog = page.getByRole("dialog");
    await expect(
      dialog.getByRole("heading", { name: "Add to Project" }),
    ).toBeVisible();
    await dialog.getByPlaceholder("DeepWiki").fill(name);
    await dialog.getByRole("button", { name: "Continue" }).click();
    await expect(
      dialog.getByRole("heading", { name: `Guardrails for ${name}` }),
    ).toBeVisible();
    await dialog.getByRole("button", { name: "Skip" }).click();

    // The confirmation shows the MCP endpoint URL: <server URL>/mcp/<slug>.
    await expect(
      dialog.getByRole("heading", { name: "Added to Project" }),
    ).toBeVisible();
    const confirmation = dialog.getByRole("link", {
      name: new RegExp(`Added to your project ${name}`),
    });
    serverPath = (await confirmation.getAttribute("href"))!;

    const { mcp_servers } = await rpc<{ mcp_servers: McpServer[] }>(
      page,
      project,
      "mcpServers.list",
    );
    const added = mcp_servers.find((s) => s.name === name);
    expect(added, `mcpServers.list includes ${name}`).toBeTruthy();
    endpointURL = added!.primary_endpoint_url!;
    expect(endpointURL).toMatch(/\/mcp\/[\w-]+$/);
    await expect(
      confirmation.getByText(endpointURL, { exact: true }),
    ).toBeVisible();
  });

  await test.step("From the confirmation, open the new server", async () => {
    await page
      .getByRole("dialog")
      .getByRole("link", { name: /View server/ })
      .click();
    await expect(page).toHaveURL(new RegExp(`${serverPath}(/|$)`));
  });

  await test.step("Connect to the MCP endpoint URL", async () => {
    // A client discovers the authorization server from the 401 challenge.
    const challenge = await page.request.post(endpointURL, { data: {} });
    expect(challenge.status()).toBe(401);
    const resourceMetadataURL = challenge
      .headers()
      ["www-authenticate"]?.match(/resource_metadata="([^"]+)"/)?.[1];
    expect(
      resourceMetadataURL,
      "401 advertises resource metadata",
    ).toBeTruthy();
    const resource = await (
      await page.request.get(resourceMetadataURL!)
    ).json();
    const issuer: string = resource.authorization_servers[0];
    const register = await page.request.post(`${issuer}/register`, {
      data: {
        client_name: name,
        redirect_uris: [redirectURI],
        token_endpoint_auth_method: "none",
        grant_types: ["authorization_code"],
        response_types: ["code"],
      },
    });
    expect(register.status()).toBeLessThan(300);
    const { client_id } = await register.json();

    const verifier = randomBytes(32).toString("base64url");
    const challengeHash = createHash("sha256")
      .update(verifier)
      .digest("base64url");
    const authorize = new URL(`${issuer}/authorize`);
    for (const [k, v] of Object.entries({
      response_type: "code",
      client_id,
      redirect_uri: redirectURI,
      code_challenge: challengeHash,
      code_challenge_method: "S256",
      state: "flow",
      resource: endpointURL,
    })) {
      authorize.searchParams.set(k, v);
    }

    let code = "";
    await page.route(`${redirectURI}**`, async (route) => {
      code = new URL(route.request().url()).searchParams.get("code") ?? "";
      await route.fulfill({ body: "ok" });
    });
    await page.goto(authorize.toString());
    await page.getByRole("button", { name: "Give access" }).click();
    await expect
      .poll(() => code, { message: "authorization code returned" })
      .not.toBe("");

    const token = await page.request.post(`${issuer}/token`, {
      form: {
        grant_type: "authorization_code",
        code,
        redirect_uri: redirectURI,
        client_id,
        code_verifier: verifier,
      },
    });
    expect(token.status()).toBeLessThan(300);
    const { access_token } = await token.json();

    const init = await page.request.post(endpointURL, {
      headers: {
        authorization: `Bearer ${access_token}`,
        accept: "application/json, text/event-stream",
      },
      data: {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: {
          protocolVersion: "2025-03-26",
          capabilities: {},
          clientInfo: { name: "flow-check", version: "1" },
        },
      },
    });
    expect(init.status()).toBe(200);
    expect(await init.text()).toContain('"protocolVersion"');
  });
});
