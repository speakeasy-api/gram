import { createContext, useContext } from "react";

export interface CommandAction {
  id: string;
  label: string;
  icon?: string;
  shortcut?: string;
  onSelect: () => void;
  group?: string;
}

interface CommandPaletteContextType {
  isOpen: boolean;
  open: () => void;
  close: () => void;
  toggle: () => void;
  actions: CommandAction[];
  setActions: (actions: CommandAction[]) => void;
  addActions: (actions: CommandAction[]) => void;
  removeActions: (ids: string[]) => void;
  contextBadge: string | null;
  setContextBadge: (badge: string | null) => void;
}

export const CommandPaletteContext = createContext<
  CommandPaletteContextType | undefined
>(undefined);

export function useCommandPalette() {
  const context = useContext(CommandPaletteContext);
  if (!context) {
    throw new Error(
      "useCommandPalette must be used within CommandPaletteProvider",
    );
  }
  return context;
}
