import { afterEach, expect, it } from "vitest";
import { restoreFleetFocus } from "./fleet-focus";
afterEach(() => {
  document.body.replaceChildren();
});
it("returns keyboard focus to the originating row after inspector close", () => {
  const row = document.createElement("button");
  row.dataset.fleetRow = "session:example-id";
  document.body.append(row);
  restoreFleetFocus("session:example-id");
  expect(document.activeElement).toBe(row);
});
it("uses the collection heading when the originating row is no longer loaded", () => {
  const heading = document.createElement("h2");
  heading.id = "fleet-collection-heading";
  heading.tabIndex = -1;
  document.body.append(heading);
  restoreFleetFocus("missing");
  expect(document.activeElement).toBe(heading);
});
