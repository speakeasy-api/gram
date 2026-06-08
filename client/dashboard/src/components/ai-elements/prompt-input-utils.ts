import type { FileUIPart } from "ai";
import { createContext, type RefObject, useContext } from "react";

// ============================================================================
// Provider Context & Types
// ============================================================================

export type AttachmentsContext = {
  files: (FileUIPart & { id: string })[];
  add: (files: File[] | FileList) => void;
  remove: (id: string) => void;
  clear: () => void;
  openFileDialog: () => void;
  fileInputRef: RefObject<HTMLInputElement | null>;
};

type TextInputContext = {
  value: string;
  setInput: (v: string) => void;
  clear: () => void;
};

export type PromptInputControllerProps = {
  textInput: TextInputContext;
  attachments: AttachmentsContext;
  /** INTERNAL: Allows PromptInput to register its file textInput + "open" callback */
  __registerFileInput: (
    ref: RefObject<HTMLInputElement | null>,
    open: () => void,
  ) => void;
};

const PromptInputController = createContext<PromptInputControllerProps | null>(
  null,
);
const ProviderAttachmentsContext = createContext<AttachmentsContext | null>(
  null,
);
export const LocalAttachmentsContext = createContext<AttachmentsContext | null>(
  null,
);

// Optional variants (do NOT throw). Useful for dual-mode components.
export const useOptionalPromptInputController =
  (): PromptInputControllerProps | null => useContext(PromptInputController);

const useOptionalProviderAttachments = () =>
  useContext(ProviderAttachmentsContext);

export const usePromptInputAttachments = (): AttachmentsContext => {
  // Dual-mode: prefer provider if present, otherwise use local
  const provider = useOptionalProviderAttachments();
  const local = useContext(LocalAttachmentsContext);
  const context = provider ?? local;
  if (!context) {
    throw new Error(
      "usePromptInputAttachments must be used within a PromptInput or PromptInputProvider",
    );
  }
  return context;
};
