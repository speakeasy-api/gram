import type { BlockRenderers } from "./blockRegistry";
import { SetupImage, SetupLink, SetupText } from "./StepBlocks";

interface TextBlock {
  type: "text";
  markdown: string;
}

interface ImageBlock {
  type: "image";
  src: string;
  alt: string;
  caption?: string;
}

interface LinkBlock {
  type: "link";
  href: string;
  label: string;
}

/**
 * Blocks any step-by-step setup can place. Their copy may come from a
 * definition the dashboard does not own, so none of them can load from or
 * inject markup for another origin.
 */
export type GenericBlock = TextBlock | ImageBlock | LinkBlock;

/** Renderers for the generic blocks, to spread into a feature's registry. */
export const genericBlockRenderers: BlockRenderers<GenericBlock> = {
  text: (block) => <SetupText markdown={block.markdown} />,
  image: (block) => (
    <SetupImage src={block.src} alt={block.alt} caption={block.caption} />
  ),
  link: (block) => <SetupLink href={block.href} label={block.label} />,
};
