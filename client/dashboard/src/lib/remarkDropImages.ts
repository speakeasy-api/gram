// Structural subset of an mdast node, enough to walk the tree.
interface MarkdownNode {
  type: string;
  children?: MarkdownNode[];
}

/**
 * Drops every image from markdown. An image in text we did not write could
 * point the viewer's browser at any host, so text never loads one.
 */
export function remarkDropImages(): (tree: unknown) => void {
  return (tree) => dropImages(tree as MarkdownNode);
}

function dropImages(node: MarkdownNode): void {
  if (node.children === undefined) return;
  node.children = node.children.filter(
    (child) => child.type !== "image" && child.type !== "imageReference",
  );
  node.children.forEach(dropImages);
}
