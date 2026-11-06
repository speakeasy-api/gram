import { test, expect, rpc, runId } from "../fixtures.js";

// Compiled from story.md.

type Toolset = { slug: string; name: string; tool_urns: string[] };

const acmeTools = [
  "check_health",
  "list_deploys",
  "fetch_traces",
  "process_refund",
  "query_db",
  "get_customer",
  "get_metrics",
  "search_logs",
].map((t) => `tools:http:acme:${t}`);

test("server-from-source", async ({ page, project }) => {
  const name = `Flow source ${runId()}`;
  const mcpPath = `/${project.orgSlug}/projects/${project.projectSlug}/mcp`;
  let slug = "";

  const toolURNs = async () => {
    const toolset = await rpc<Toolset>(page, project, "toolsets.get", {
      query: { slug },
    });
    return [...toolset.tool_urns].sort();
  };

  await test.step("From the MCP servers list, start adding a new server", async () => {
    await page.goto(mcpPath);
    await page.getByRole("button", { name: "Add new" }).click();
    await expect(page).toHaveURL(new RegExp(`${mcpPath}/add$`));
  });

  await test.step("Choose to build it from an existing source", async () => {
    await page.getByRole("link", { name: /^From an existing source/ }).click();
    await expect(page).toHaveURL(
      new RegExp(`${mcpPath}/add/from-existing-source$`),
    );
  });

  await test.step("Pick the Acme Internal API source and name the server", async () => {
    await page.getByRole("button", { name: /^Acme Internal API/ }).click();
    await page.getByLabel("Server name").fill(name);
  });

  await test.step("Create the server", async () => {
    await page.getByRole("button", { name: "Create" }).click();
    // The user lands on the new server's tools page.
    await expect(page).toHaveURL(new RegExp(`${mcpPath}/[^/]+/tools$`));

    const { toolsets } = await rpc<{ toolsets: Toolset[] }>(
      page,
      project,
      "toolsets.list",
    );
    const created = toolsets.find((t) => t.name === name);
    expect(created, `toolset named ${name}`).toBeTruthy();
    slug = created!.slug;

    await expect.poll(toolURNs).toEqual([...acmeTools].sort());
  });

  await test.step("On the new server's tools, remove the check_health tool", async () => {
    // The tool rows have no role, so scope to the closest block that holds
    // the tool name with both its checkbox and its menu button.
    const row = page
      .getByRole("main")
      .getByText("check_health", { exact: true })
      .locator("xpath=ancestor::div[count(.//button)>=2][1]");
    await row.getByRole("button", { name: "Open menu" }).click();
    await page.getByRole("menuitem", { name: "Remove" }).click();

    await expect
      .poll(toolURNs)
      .toEqual(
        acmeTools.filter((u) => u !== "tools:http:acme:check_health").sort(),
      );
  });

  await test.step("Add the check_health tool back", async () => {
    await page.getByRole("button", { name: "Add Tools" }).click();
    const dialog = page.getByRole("dialog");
    await dialog
      .getByRole("textbox", { name: "Search tools..." })
      .fill("check_health");
    await dialog
      .getByText("check_health", { exact: true })
      .locator("xpath=ancestor::div[.//*[@role='checkbox']][1]")
      .getByRole("checkbox")
      .click();
    await dialog.getByRole("button", { name: "Add 1 Tool" }).click();

    await expect.poll(toolURNs).toEqual([...acmeTools].sort());
  });
});
