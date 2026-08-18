import { useCallback, useState } from "react";

// Appended to every other announcement so an unchanged sentence still changes
// the text node. Zero-width, so nothing is spoken and nothing takes up space.
const ZERO_WIDTH_SPACE = "\u200b";

// A live region is announced when its text changes. The same sentence twice
// sets the same string, React bails on the equal value, the DOM text never
// moves, and the operator hears nothing: a refusal repeated, or a second write
// that fails the way the first one did, is spoken once. The count rides along
// so the text differs even where the sentence does not.
export function useAnnouncer(): {
  announce: (text: string) => void;
  announced: string;
} {
  const [announcement, setAnnouncement] = useState({ text: "", count: 0 });

  const announce = useCallback((text: string): void => {
    setAnnouncement((previous) => ({ text, count: previous.count + 1 }));
  }, []);

  return {
    announce,
    announced:
      announcement.text +
      (announcement.count % 2 === 1 ? ZERO_WIDTH_SPACE : ""),
  };
}
