import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type RefCallback,
  type RefObject,
} from "react";
import type { EmaAppAssignment } from "@/lib/devidp";
import {
  buildRouteGeometry,
  EMPTY_ROUTE_GEOMETRY,
  type RouteGeometry,
} from "@/lib/ema-pipeline";

export interface EmaLayout extends RouteGeometry {
  registerApp: (id: string) => RefCallback<HTMLElement>;
  registerUser: (id: string) => RefCallback<HTMLElement>;
  registerResource: (id: string) => RefCallback<HTMLElement>;
}

/**
 * Single source of truth for the assignment graph's layout.
 *
 * Same machinery as useMembershipLayout — one ResizeObserver, callback refs
 * cached per id, recompute scheduled on a frame — with three element maps
 * instead of two, because a route touches three cards.
 */
export function useEmaLayout(
  containerRef: RefObject<HTMLElement | null>,
  assignments: EmaAppAssignment[],
): EmaLayout {
  const appEls = useRef(new Map<string, HTMLElement>());
  const userEls = useRef(new Map<string, HTMLElement>());
  const resourceEls = useRef(new Map<string, HTMLElement>());
  const appRefCache = useRef(new Map<string, RefCallback<HTMLElement>>());
  const userRefCache = useRef(new Map<string, RefCallback<HTMLElement>>());
  const resourceRefCache = useRef(new Map<string, RefCallback<HTMLElement>>());
  const observer = useRef<ResizeObserver | null>(null);
  const rafId = useRef<number | null>(null);
  const [tick, setTick] = useState(0);

  const schedule = useCallback(() => {
    if (rafId.current !== null) return;
    rafId.current = requestAnimationFrame(() => {
      rafId.current = null;
      setTick((t) => t + 1);
    });
  }, []);

  useLayoutEffect(() => {
    const ro = new ResizeObserver(() => schedule());
    observer.current = ro;
    if (containerRef.current) ro.observe(containerRef.current);
    appEls.current.forEach((el) => ro.observe(el));
    userEls.current.forEach((el) => ro.observe(el));
    resourceEls.current.forEach((el) => ro.observe(el));
    return () => {
      ro.disconnect();
      observer.current = null;
    };
  }, [containerRef, schedule]);

  useEffect(() => {
    const onShift = () => schedule();
    window.addEventListener("resize", onShift);
    window.addEventListener("scroll", onShift, true);
    return () => {
      window.removeEventListener("resize", onShift);
      window.removeEventListener("scroll", onShift, true);
    };
  }, [schedule]);

  const makeRegistrar = useCallback(
    (
      els: RefObject<Map<string, HTMLElement>>,
      cache: RefObject<Map<string, RefCallback<HTMLElement>>>,
    ) =>
      (id: string): RefCallback<HTMLElement> => {
        const cached = cache.current.get(id);
        if (cached) return cached;
        const cb: RefCallback<HTMLElement> = (el) => {
          const prev = els.current.get(id);
          if (prev === el) return;
          if (prev) observer.current?.unobserve(prev);
          if (el) {
            els.current.set(id, el);
            observer.current?.observe(el);
          } else {
            els.current.delete(id);
            cache.current.delete(id);
          }
          schedule();
        };
        cache.current.set(id, cb);
        return cb;
      },
    [schedule],
  );

  const registerApp = useMemo(
    () => makeRegistrar(appEls, appRefCache),
    [makeRegistrar],
  );
  const registerUser = useMemo(
    () => makeRegistrar(userEls, userRefCache),
    [makeRegistrar],
  );
  const registerResource = useMemo(
    () => makeRegistrar(resourceEls, resourceRefCache),
    [makeRegistrar],
  );

  const geometry = useMemo<RouteGeometry>(() => {
    if (!containerRef.current) return EMPTY_ROUTE_GEOMETRY;
    return buildRouteGeometry(assignments, containerRef.current, {
      apps: appEls.current,
      users: userEls.current,
      resources: resourceEls.current,
    });
    // tick is the layout-version trigger; refs are mutated in place so they
    // can't be deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [assignments, tick, containerRef]);

  return { ...geometry, registerApp, registerUser, registerResource };
}
