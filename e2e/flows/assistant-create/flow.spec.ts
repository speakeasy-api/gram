import { test, expect, rpc, runId } from "../fixtures.js";

// Compiled from story.md. The builder is an LLM, so its waits are long.

type Assistant = { id: string; name: string };
type ChatMessage = { role: string; content?: string };

test("assistant-create", async ({ page, project }) => {
  const name = `Flow bot ${runId()}`;
  const listPath = `/${project.orgSlug}/projects/${project.projectSlug}/assistants`;

  await test.step("From the assistants list, start a new assistant", async () => {
    await page.goto(listPath);
    await page.getByRole("button", { name: "New Assistant" }).click();
    await expect(page).toHaveURL(new RegExp(`${listPath}/new`));
  });

  await test.step("Ask the builder to create a docs assistant with no tools or triggers", async () => {
    const input = page.getByRole("textbox", { name: "Message input" });
    await input.fill(
      `Create an assistant named \`${name}\` that answers questions about our docs. Do not add any tools or triggers.`,
    );
    await page.getByRole("button", { name: "Send message" }).click();
  });

  let assistantId = "";
  await test.step("Wait for the builder to create the assistant", async () => {
    // The URL gains the new assistant's id once the builder creates it.
    await expect(page).toHaveURL(/[?&]id=[0-9a-f-]{36}/, { timeout: 150_000 });
    const url = new URL(page.url());
    assistantId = url.searchParams.get("id")!;
    const threadId = url.searchParams.get("threadId")!;
    expect(threadId).toBeTruthy();

    // The builder replies in the chat.
    await expect
      .poll(
        async () => {
          const chat = await rpc<{ messages: ChatMessage[] }>(
            page,
            project,
            "chat.load",
            {
              query: { id: threadId },
            },
          );
          return chat.messages.some((m) => m.role === "assistant");
        },
        { timeout: 150_000 },
      )
      .toBe(true);

    await expect
      .poll(
        async () => {
          const { assistants } = await rpc<{ assistants: Assistant[] }>(
            page,
            project,
            "assistants.list",
          );
          return assistants.find((a) => a.id === assistantId)?.name;
        },
        { timeout: 60_000 },
      )
      .toBe(name);
  });

  await test.step("Go back to the assistants list", async () => {
    await page.getByRole("link", { name: /^Assistants/ }).click();
    await expect(page).toHaveURL(new RegExp(`${listPath}$`));
    await expect(
      page.getByRole("link", { name: new RegExp(name) }),
    ).toBeVisible();
  });
});
