/**
 * Caret and text plumbing for the contenteditable composer.
 *
 * A contenteditable holds a tree, not a string, so every operation the rest of
 * the composer does against a textarea — read `value`, read `selectionStart`,
 * move the caret — has to be recomputed by walking that tree. Keeping it here
 * means the input component stays about rendering and the walk stays testable.
 */

/** The draft as a plain string: text nodes in order, `<br>` as a newline. */
export function readPlainText(root: HTMLElement): string {
  let text = "";
  const walk = (node: Node) => {
    for (const child of node.childNodes) {
      if (child.nodeType === Node.TEXT_NODE) {
        text += child.nodeValue ?? "";
      } else if (child.nodeName === "BR") {
        text += "\n";
      } else {
        walk(child);
      }
    }
  };
  walk(root);
  return text;
}

/**
 * How many characters precede (node, offset), counting the same way
 * `readPlainText` does — this is the contenteditable's `selectionStart`.
 *
 * Measured by cloning everything before the caret rather than walking by hand:
 * the browser already knows how to slice a tree at an arbitrary boundary, and
 * a clone can't disagree with the live DOM about what came first.
 */
export function offsetOf(
  root: HTMLElement,
  node: Node,
  nodeOffset: number,
): number {
  const range = document.createRange();
  range.setStart(root, 0);
  range.setEnd(node, nodeOffset);
  const holder = document.createElement("div");
  holder.appendChild(range.cloneContents());
  return readPlainText(holder).length;
}

/** Chrome scopes selection to the shadow root; other engines answer from the
 *  document even for nodes inside one. Ask the closer object first. */
type SelectionRoot = { getSelection?: () => Selection | null };

function selectionIn(root: HTMLElement): Selection | null {
  const scope = root.getRootNode() as unknown as SelectionRoot;
  return scope.getSelection?.() ?? document.getSelection();
}

/** The caret's character offset in `root`, or null when it isn't inside it. */
export function caretOffset(root: HTMLElement): number | null {
  const selection = selectionIn(root);
  const range = selection?.rangeCount ? selection.getRangeAt(0) : null;
  if (!range || !root.contains(range.startContainer)) return null;
  return offsetOf(root, range.startContainer, range.startOffset);
}

/** The current selection as character offsets, or null when it isn't in `root`. */
export function selectionOffsets(
  root: HTMLElement,
): { start: number; end: number } | null {
  const selection = selectionIn(root);
  const range = selection?.rangeCount ? selection.getRangeAt(0) : null;
  if (!range || !root.contains(range.startContainer)) return null;
  return {
    start: offsetOf(root, range.startContainer, range.startOffset),
    end: offsetOf(root, range.endContainer, range.endOffset),
  };
}

/** Where a character offset lands in the tree, clamped to the end. */
export function positionAt(
  root: HTMLElement,
  offset: number,
): { node: Node; offset: number } {
  let remaining = offset;
  let last: { node: Node; offset: number } = { node: root, offset: 0 };

  const walk = (current: Node): { node: Node; offset: number } | null => {
    for (const child of current.childNodes) {
      if (child.nodeType === Node.TEXT_NODE) {
        const length = child.nodeValue?.length ?? 0;
        if (remaining <= length) return { node: child, offset: remaining };
        remaining -= length;
        last = { node: child, offset: length };
      } else if (child.nodeName === "BR") {
        if (remaining === 0) {
          return { node: current, offset: indexOfChild(current, child) };
        }
        remaining -= 1;
        last = { node: current, offset: indexOfChild(current, child) + 1 };
      } else {
        const hit = walk(child);
        if (hit) return hit;
      }
    }
    return null;
  };

  return walk(root) ?? last;
}

function indexOfChild(parent: Node, child: Node): number {
  return Array.prototype.indexOf.call(parent.childNodes, child);
}

/** Puts the caret at a character offset (or selects a range). */
export function setCaret(root: HTMLElement, start: number, end = start): void {
  const selection = selectionIn(root);
  if (!selection) return;

  const from = positionAt(root, start);
  const to = end === start ? from : positionAt(root, end);
  const range = document.createRange();
  range.setStart(from.node, from.offset);
  range.setEnd(to.node, to.offset);
  selection.removeAllRanges();
  selection.addRange(range);
}
