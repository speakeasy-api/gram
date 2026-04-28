export const SECTION_NAMES = ["Personality", "Behavior", "Tasks"] as const;
export type SectionName = (typeof SECTION_NAMES)[number];

const HEADING_RE = /^# (Personality|Behavior|Tasks)[ \t]*\r?$/gm;

type SectionRange = {
  name: SectionName;
  start: number;
  contentStart: number;
  end: number;
};

function findSections(text: string): SectionRange[] {
  HEADING_RE.lastIndex = 0;
  const found: Omit<SectionRange, "end">[] = [];
  for (let m: RegExpExecArray | null; (m = HEADING_RE.exec(text)); ) {
    found.push({
      name: m[1] as SectionName,
      start: m.index,
      contentStart: m.index + m[0].length,
    });
  }
  return found.map((r, i) => ({
    ...r,
    end: found[i + 1]?.start ?? text.length,
  }));
}

export function getSection(text: string, name: SectionName): string | null {
  const ranges = findSections(text);
  const r = ranges.find((x) => x.name === name);
  if (!r) return null;
  let s = r.contentStart;
  if (text[s] === "\n") s += 1;
  return text.slice(s, r.end).replace(/\s+$/, "");
}

export function setSection(
  text: string,
  name: SectionName,
  content: string,
): string {
  const trimmed = content.trim();
  const block = trimmed.length > 0 ? `# ${name}\n${trimmed}` : `# ${name}`;
  const ranges = findSections(text);
  const r = ranges.find((x) => x.name === name);
  if (r) {
    const before = text.slice(0, r.start).replace(/\s+$/, "");
    const after = text.slice(r.end).replace(/^\s+/, "");
    return [before, block, after].filter((x) => x.length > 0).join("\n\n");
  }
  const head = text.replace(/\s+$/, "");
  if (head.length === 0) return block;
  return `${head}\n\n${block}`;
}

export function removeSection(text: string, name: SectionName): string {
  const ranges = findSections(text);
  const r = ranges.find((x) => x.name === name);
  if (!r) return text;
  const before = text.slice(0, r.start).replace(/\s+$/, "");
  const after = text.slice(r.end).replace(/^\s+/, "");
  return [before, after].filter((x) => x.length > 0).join("\n\n");
}
