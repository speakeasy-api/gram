import { randomUUID } from "node:crypto";
import { writeFile } from "node:fs/promises";
import type { Page, TestInfo } from "@playwright/test";

/** Complete exceptions stay in ignored mode-0600 files, never report attachments. */
export function privateProtocolFailureRecorder(info: TestInfo) {
  let count = 0;
  return async (event: { operation: string; body: unknown }) => {
    if (count++ >= 32) return;
    await writeFile(
      info.outputPath(`private-protocol-failure-${randomUUID()}.json`),
      JSON.stringify(event),
      { mode: 0o600 },
    );
  };
}

/** Capture the consent page at failure, before teardown navigates away from it. */
export async function privateConsentSnapshot(page: Page, info: TestInfo) {
  const url = new URL(page.url());
  await writeFile(
    info.outputPath(`private-consent-snapshot-${randomUUID()}.yaml`),
    `# ${url.origin}${url.pathname}\n${await page.locator("body").ariaSnapshot()}\n`,
    { mode: 0o600 },
  );
}

/** Read the rendered error contract without exposing sessions or raw messages. */
export async function authorizationFailure(page: Page) {
  const details = page.locator("details[data-error-code]");
  if ((await details.count()) !== 1 || !(await details.isVisible()))
    return null;
  const code = await details.getAttribute("data-error-code");
  const description =
    (await details.getAttribute("data-error-description")) ?? "";
  const knownCodes = [
    "unauthorized",
    "forbidden",
    "bad_request",
    "unexpected",
    "access_denied",
    "invalid_client",
    "unauthorized_client",
    "invalid_request",
    "invalid_scope",
    "invalid_target",
    "server_error",
    "temporarily_unavailable",
    "unsupported_response_type",
  ];
  const knownReasons = [
    "the identity provider rejected the requested resource",
    "typed connection authorization is no longer valid",
  ];
  return {
    code:
      code !== null && knownCodes.includes(code)
        ? code
        : "unrecognized_or_missing",
    reason:
      knownReasons.find((reason) => description === reason) ?? "unclassified",
    descriptionLength: description.length,
    markers: [
      "resource",
      "scope",
      "client",
      "permission",
      "revoked",
      "expired",
      "sign-in",
      "provider",
    ].filter((marker) => description.toLowerCase().includes(marker)),
  };
}
