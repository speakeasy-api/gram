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
import type { Membership } from "@/lib/devidp";
import {
  buildGeometry,
  EMPTY_GEOMETRY,
  type Geometry,
} from "@/lib/membership-pipeline";

export interface MembershipLayout extends Geometry {
  registerOrg: (id: string) => RefCallback<HTMLElement>;
  registerUser: (id: string) => RefCallback<HTMLElement>;
}

/**
 * Single source of truth for the membership graph's layout.
 *
 * Owns two element maps (org/user) plus one ResizeObserver. Recomputation is
 * triggered by:
 *
 *   - container resize
 *   - any tracked card resize
 *   - card mount/unmount (callback-ref add/remove)
 *   - window resize / scroll (parent layout shifts)
 *   - new memberships
 *
 * Per-id ref callbacks are cached so React doesn't see ref churn between
 * renders for the same card.
 */
export function useMembershipLayout(
  containerRef: RefObject<HTMLElement | null>,
  memberships: Membership[],
): MembershipLayout {
  const orgEls = useRef(new Map<string, HTMLElement>());
  const userEls = useRef(new Map<string, HTMLElement>());
  const orgRefCache = useRef(new Map<string, RefCallback<HTMLElement>>());
  const userRefCache = useRef(new Map<string, RefCallback<HTMLElement>>());
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

  // Single ResizeObserver, attached to the container and every tracked card.
  useLayoutEffect(() => {
    const ro = new ResizeObserver(() => schedule());
    observer.current = ro;
    if (containerRef.current) ro.observe(containerRef.current);
    orgEls.current.forEach((el) => ro.observe(el));
    userEls.current.forEach((el) => ro.observe(el));
    return () => {
      ro.disconnect();
      observer.current = null;
    };
  }, [containerRef, schedule]);

  // Window-level layout shifts (parent reflow, page scroll).
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

  const registerOrg = useMemo(
    () => makeRegistrar(orgEls, orgRefCache),
    [makeRegistrar],
  );
  const registerUser = useMemo(
    () => makeRegistrar(userEls, userRefCache),
    [makeRegistrar],
  );

  const geometry = useMemo<Geometry>(() => {
    if (!containerRef.current) return EMPTY_GEOMETRY;
    return buildGeometry(memberships, containerRef.current, {
      orgs: orgEls.current,
      users: userEls.current,
    });
    // tick is the layout-version trigger; refs are mutated in place so they
    // can't be deps.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [memberships, tick, containerRef]);

  return { ...geometry, registerOrg, registerUser };
}
