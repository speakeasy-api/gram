import { describe, expect, it } from "vitest";

import { positionAt, readPlainText, selectionOffsets } from "./composer-dom";

/** `ask @search_docs now`, with the reference as a chip like the composer builds. */
function draftWithChip(): HTMLElement {
  const root = document.createElement("div");
  root.append(document.createTextNode("ask "));
  const chip = document.createElement("span");
  chip.contentEditable = "false";
  chip.dataset.reference = "tool";
  chip.textContent = "@search_docs";
  root.append(chip, document.createTextNode(" now"));
  return root;
}

describe("readPlainText", () => {
  it("reads chips as their text", () => {
    expect(readPlainText(draftWithChip())).toBe("ask @search_docs now");
  });
});

describe("positionAt", () => {
  it("lands before a chip", () => {
    const root = draftWithChip();
    expect(positionAt(root, 4)).toEqual({
      node: root.childNodes[0],
      offset: 4,
    });
  });

  it("never lands inside a chip", () => {
    const root = draftWithChip();
    const chip = root.childNodes[1]!;
    for (let offset = 5; offset < 16; offset++) {
      const position = positionAt(root, offset);
      expect(position.node).not.toBe(chip);
      expect(chip.contains(position.node)).toBe(false);
    }
  });

  it("snaps an offset inside a chip to the nearer boundary", () => {
    const root = draftWithChip();
    expect(positionAt(root, 5)).toEqual({ node: root, offset: 1 });
    expect(positionAt(root, 15)).toEqual({ node: root, offset: 2 });
  });

  it("lands after a chip", () => {
    const root = draftWithChip();
    expect(positionAt(root, 16)).toEqual({
      node: root.childNodes[2],
      offset: 0,
    });
  });
});

describe("selectionOffsets", () => {
  it("clamps a selection that runs past the composer", () => {
    const page = document.createElement("div");
    const root = draftWithChip();
    const outside = document.createElement("p");
    outside.textContent = "page text";
    page.append(root, outside);
    document.body.append(page);

    const range = document.createRange();
    range.setStart(root.childNodes[0]!, 1);
    range.setEnd(outside.firstChild!, 5);
    const selection = document.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);

    expect(selectionOffsets(root)).toEqual({ start: 1, end: 20 });
    page.remove();
  });
});
