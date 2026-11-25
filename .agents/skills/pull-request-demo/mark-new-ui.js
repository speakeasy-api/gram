// Rings every [data-demo-new] element and tags it with the attribute's value.
// Declaration, not arrow: run-code parenthesizes the file and a trailing ; breaks it.
async function markNewUi(page) {
  const count = await page.evaluate(() => {
    document.querySelectorAll("[data-demo-mark]").forEach((n) => n.remove());
    const targets = [...document.querySelectorAll("[data-demo-new]")];
    const pad = 6;
    let marked = 0;
    for (const el of targets) {
      const r = el.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) continue;
      const top = r.top + window.scrollY - pad;
      const left = r.left + window.scrollX - pad;
      const layer = {
        position: "absolute",
        zIndex: "2147483647",
        pointerEvents: "none",
      };
      const ring = document.createElement("div");
      ring.dataset.demoMark = "";
      Object.assign(ring.style, layer, {
        top: `${top}px`,
        left: `${left}px`,
        width: `${r.width + pad * 2}px`,
        height: `${r.height + pad * 2}px`,
        border: "3px solid #e11d48",
        borderRadius: "8px",
        boxShadow: "0 0 0 4px rgba(225, 29, 72, 0.2)",
      });
      const tag = document.createElement("div");
      tag.dataset.demoMark = "";
      tag.textContent = el.dataset.demoNew || "New";
      Object.assign(tag.style, layer, {
        top: `${Math.max(top - 11, 2)}px`,
        left: `${left + 10}px`,
        background: "#e11d48",
        color: "#fff",
        font: "600 12px/1 system-ui, sans-serif",
        padding: "4px 7px",
        borderRadius: "5px",
        whiteSpace: "nowrap",
      });
      document.body.append(ring, tag);
      marked++;
    }
    return marked;
  });
  return `marked ${count} element(s)`;
}
