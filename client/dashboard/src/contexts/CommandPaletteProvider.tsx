import {
  useState,
  type ReactNode,
  useEffect,
  useCallback,
  useMemo,
} from "react";
import { CommandPaletteContext } from "./CommandPalette";
import type { CommandAction } from "./CommandPalette";

export function CommandPaletteProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [actions, setActionsState] = useState<CommandAction[]>([]);
  const [contextBadge, setContextBadgeState] = useState<string | null>(null);

  const open = useCallback(() => setIsOpen(true), []);
  const close = useCallback(() => setIsOpen(false), []);
  const toggle = useCallback(() => setIsOpen((prev) => !prev), []);

  const setContextBadge = useCallback((badge: string | null) => {
    setContextBadgeState(badge);
  }, []);

  const setActions = useCallback((newActions: CommandAction[]) => {
    setActionsState(newActions);
  }, []);

  const addActions = useCallback((newActions: CommandAction[]) => {
    setActionsState((prev) => {
      const existing = new Map(prev.map((a) => [a.id, a]));
      newActions.forEach((action) => existing.set(action.id, action));
      return Array.from(existing.values());
    });
  }, []);

  const removeActions = useCallback((ids: string[]) => {
    setActionsState((prev) =>
      prev.filter((action) => !ids.includes(action.id)),
    );
  }, []);

  // Global keyboard shortcut
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        toggle();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [toggle]);

  const value = useMemo(
    () => ({
      isOpen,
      open,
      close,
      toggle,
      actions,
      setActions,
      addActions,
      removeActions,
      contextBadge,
      setContextBadge,
    }),
    [
      isOpen,
      open,
      close,
      toggle,
      actions,
      setActions,
      addActions,
      removeActions,
      contextBadge,
      setContextBadge,
    ],
  );

  return (
    <CommandPaletteContext.Provider value={value}>
      {children}
    </CommandPaletteContext.Provider>
  );
}
