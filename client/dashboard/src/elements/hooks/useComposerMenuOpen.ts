import { RefObject, useEffect } from "react";

/** Marks the Elements shadow host while a composer menu (slash commands, tool
 *  mentions) is open.
 *
 *  Those menus are absolutely positioned and open past the composer's own box.
 *  A host that clips the composer — the docked pill animates it inside an
 *  `overflow: hidden` grid-rows tray — crops them. The host page can relax that
 *  clip with `:has([data-composer-menu-open])`, but only because the shadow
 *  HOST is in its light tree: no selector can reach the menu itself.
 */
export function useComposerMenuOpen(
  open: boolean,
  ref: RefObject<HTMLElement | null>,
): void {
  useEffect(() => {
    if (!open) return;
    const root = ref.current?.getRootNode();
    const host = root instanceof ShadowRoot ? root.host : null;
    if (!(host instanceof HTMLElement)) return;

    // Counted, because more than one menu can be open at once (a slash command
    // and an @-mention in the same draft). A plain attribute would let the
    // first one to close strip the clip-lift from the one still on screen.
    openMenus.set(host, (openMenus.get(host) ?? 0) + 1);
    host.toggleAttribute("data-composer-menu-open", true);
    return () => {
      const remaining = (openMenus.get(host) ?? 1) - 1;
      openMenus.set(host, Math.max(remaining, 0));
      if (remaining <= 0) host.removeAttribute("data-composer-menu-open");
    };
  }, [open, ref]);
}

/** Open menus per shadow host. */
const openMenus = new WeakMap<HTMLElement, number>();
