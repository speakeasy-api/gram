import {
  useState,
  type ReactNode,
  useEffect,
  useCallback,
  useMemo,
  useRef,
} from "react";
import { CommandPaletteContext } from "./CommandPalette";
import type { CommandAction } from "./CommandPalette";

const MAX_FOCUS_RESTORE_FRAMES = 60;

function isVisiblyRendered(element: HTMLElement): boolean {
  let current: HTMLElement | null = element;

  while (current) {
    const style = window.getComputedStyle(current);
    if (
      style.display === "none" ||
      style.visibility === "hidden" ||
      style.visibility === "collapse" ||
      Number.parseFloat(style.opacity) === 0 ||
      style.contentVisibility === "hidden"
    ) {
      return false;
    }

    current = current.parentElement;
  }

  return element.getClientRects().length > 0;
}

function isUsableFocusTarget(
  element: HTMLElement | null,
): element is HTMLElement {
  if (!element?.isConnected) return false;

  return (
    !element.closest(
      ':disabled, [disabled], [aria-disabled="true"], [hidden], [inert], [aria-hidden="true"]',
    ) && isVisiblyRendered(element)
  );
}

function focusIfUsable(element: HTMLElement | null): boolean {
  if (!isUsableFocusTarget(element)) return false;

  element.focus();
  return document.activeElement === element;
}

export function CommandPaletteProvider({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const [isOpen, setIsOpen] = useState(false);
  const [actions, setActionsState] = useState<CommandAction[]>([]);
  const [contextBadge, setContextBadgeState] = useState<string | null>(null);
  const openerRef = useRef<HTMLElement | null>(null);

  const open = useCallback(() => {
    const activeElement = document.activeElement;
    openerRef.current =
      activeElement instanceof HTMLElement && activeElement !== document.body
        ? activeElement
        : null;
    setIsOpen(true);
  }, []);

  const close = useCallback(() => {
    setIsOpen(false);
    const opener = openerRef.current;

    const restoreFocus = (frame: number) => {
      if (openerRef.current !== opener) return;

      if (!focusIfUsable(opener)) {
        const fallback = Array.from(
          document.querySelectorAll<HTMLElement>(
            '[data-slot="command-palette-trigger"]',
          ),
        ).find(isUsableFocusTarget);

        if (focusIfUsable(fallback ?? null)) {
          openerRef.current = null;
          return;
        }

        if (frame < MAX_FOCUS_RESTORE_FRAMES) {
          requestAnimationFrame(() => restoreFocus(frame + 1));
          return;
        }
      }

      openerRef.current = null;
    };

    requestAnimationFrame(() => restoreFocus(0));
  }, []);

  const toggle = useCallback(() => {
    if (isOpen) {
      close();
    } else {
      open();
    }
  }, [close, isOpen, open]);

  const setContextBadge = useCallback((badge: string | null) => {
    setContextBadgeState(badge);
  }, []);

  const setActions = useCallback((newActions: CommandAction[]) => {
    setActionsState(newActions);
  }, []);

  const addActions = useCallback((newActions: CommandAction[]) => {
    setActionsState((prev) => {
      const existing = new Map(prev.map((a) => [a.id, a]));
      newActions.forEach((action) => {
        void existing.set(action.id, action);
      });
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
