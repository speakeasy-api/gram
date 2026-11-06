import { test, type Page } from "@playwright/test";
import type { AuthGraph } from "./graph.ts";
import type { GatewayInventoryObservation } from "./gateway.ts";

export function inventoryObserver(phase: string) {
  return async (inventory: GatewayInventoryObservation) => {
    await test.info().attach(`gateway-inventory-${phase}`, {
      body: JSON.stringify(inventory),
      contentType: "application/json",
    });
  };
}

// Called before each real consent action. Read only booleans for expected fixture members.
export function consentObserver(page: Page, slugs: string[], phase: string) {
  let checkpoint = 0;
  return async () => {
    const expected = [];
    for (const [index, slug] of slugs.entries()) {
      const escaped = slug.replace(/[\\"]/g, "\\$&");
      const row = page.locator(
        `[data-gateway-server="${escaped}"], [data-gateway-server$=".${escaped}"]`,
      );
      const count = await row.count();
      if (count > 1)
        throw new Error("Gateway consent has ambiguous expected member rows");
      const select = row.locator('select[name^="auth_client."]');
      const options = count === 1 ? await select.locator("option").all() : [];
      const selected =
        count === 1 ? await select.locator("option:checked").all() : [];
      let anyUsableOption = false;
      let personalOptionPresent = false;
      let personalOptionSelected = false;
      let selectedUsableOption = false;
      for (const option of options) {
        anyUsableOption ||= !(await option.isDisabled());
        personalOptionPresent ||=
          (await option.textContent())?.includes(" · Your own account") ??
          false;
      }
      for (const option of selected) {
        selectedUsableOption ||= !(await option.isDisabled());
        personalOptionSelected ||=
          (await option.textContent())?.includes(" · Your own account") ??
          false;
      }
      expected.push({
        member: index + 1,
        present: count === 1,
        ready:
          count === 1 &&
          (await row.getAttribute("data-gateway-server-ready")) === "true",
        hidden:
          count === 1 &&
          (await row.getAttribute("data-gateway-server-hidden")) !== null,
        pickerPresent: (await select.count()) === 1,
        anyUsableOption,
        personalOptionPresent,
        personalOptionSelected,
        selectedUsableOption,
      });
    }
    await test.info().attach(`gateway-consent-${phase}-${checkpoint++}`, {
      body: JSON.stringify({ expected }),
      contentType: "application/json",
    });
  };
}

// Exclude newly issued session selections; retain source identity and credential bindings.
export function sourceTopology(graph: AuthGraph, names: string[]) {
  return names.map((name) => {
    const server = graph.servers.find((value) => value.name === name);
    if (!server)
      throw new Error("Gateway control source missing from fixture graph");
    return {
      server: server.server,
      signInIssuer: server.signInIssuer,
      derivedUpstreamIssuer: server.derivedUpstreamIssuer,
      disabled: server.disabled,
      deleted: server.deleted,
      offers: server.offers
        .map((offer) => ({
          client: offer.client,
          type: offer.type,
          source: offer.source,
          bindingSource: offer.bindingSource,
          enabled: offer.enabled,
          deleted: offer.deleted,
          bindingDeleted: offer.bindingDeleted,
          registration: offer.registration,
          issuer: offer.issuer,
          issuerBridge: offer.issuerBridge,
          directLegacyAssociation: offer.directLegacyAssociation,
        }))
        .sort((a, b) => a.client.localeCompare(b.client)),
    };
  });
}
