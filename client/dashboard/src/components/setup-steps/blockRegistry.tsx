import { Fragment, type ReactNode } from "react";

/** Any block a setup can place: a discriminated union on `type`. */
export interface Block {
  type: string;
}

/**
 * How a feature renders each of its block types. Each renderer receives the
 * block narrowed to its own type, and the block's key: unique within the
 * setup, for state the feature keeps per block.
 */
export type BlockRenderers<B extends Block> = {
  [T in B["type"]]: (
    block: Extract<B, { type: T }>,
    blockKey: string,
  ) => ReactNode;
};

/** Renders one block with the renderer registered for its type. */
function renderBlock<B extends Block>(
  renderers: BlockRenderers<B>,
  block: B,
  blockKey: string,
): ReactNode {
  // TypeScript cannot correlate block.type with the renderer it selects; the
  // mapped type guarantees the renderer for a type accepts blocks of it.
  const render = renderers[block.type as B["type"]] as (
    block: B,
    blockKey: string,
  ) => ReactNode;
  return render(block, blockKey);
}

/**
 * A step's blocks, in order. Each block's key is `${keyPrefix}-${index}`, so
 * prefix with something unique per step, such as its id.
 */
export function BlockList<B extends Block>({
  blocks,
  renderers,
  keyPrefix,
}: {
  blocks: readonly B[];
  renderers: BlockRenderers<B>;
  keyPrefix: string;
}): JSX.Element {
  return (
    <>
      {blocks.map((block, index) => (
        <Fragment key={index}>
          {renderBlock(renderers, block, `${keyPrefix}-${index}`)}
        </Fragment>
      ))}
    </>
  );
}
