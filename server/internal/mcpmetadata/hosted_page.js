// Tool-filtering scope state. scopeVariants is the JSON map (keyed by tag, with
// the empty-string key holding the unfiltered defaults) emitted server-side when
// filtering is enabled; it stays null on servers without a variations group, in
// which case all scope behavior is inert and the page works exactly as before.
let scopeVariants = null;
let defaultScopeUrl = "";
let activeScopeTag = "";
let toolSearchQuery = "";

function togglePopover(e) {
  const popoverRoot = e.currentTarget.parentElement;
  const menu = popoverRoot.querySelector(".popover-menu");
  if (menu.classList.contains("hidden")) {
    e.stopPropagation();
    menu.classList.replace("hidden", "active");
    const popoverListener = window.addEventListener("click", (event) => {
      if (menu.contains(event.target) || event.target === menu) {
        return;
      }
      menu.classList.replace("active", "hidden");
      window.removeEventListener("click", popoverListener);
    });
  }
}

function closeModal() {
  const modal = document.querySelector(".modal");
  const backdrop = document.querySelector(".modal-backdrop");
  modal.classList.add("fade-out");
  backdrop.classList.add("fade-out");
  modal.addEventListener("transitionend", () => {
    modal.remove();
    backdrop.remove();
  });
}

function openModal(childContent) {
  const template = document.querySelector("#modal-template");
  const modalElement = template.content.cloneNode(true);

  // The cloned snippets carry the unfiltered default URL; re-apply the active
  // scope so the modal reflects the currently selected chip.
  applyScopeToTree(childContent, activeScopeTag);

  modalElement.querySelector(".content-slot").appendChild(childContent);
  modalElement.querySelectorAll(".code-container").forEach((el) => {
    el.addEventListener("click", copyContainerSnippet);
  });

  document.body.appendChild(modalElement);
  const modal = document.querySelector(".modal");
  const backdrop = document.querySelector(".modal-backdrop");
  modal.addEventListener("animationend", () => {
    modal.classList.remove("fade-in");
    backdrop.classList.remove("fade-in");
  });
  modal.classList.add("fade-in");
  backdrop.classList.add("fade-in");
  backdrop.addEventListener("click", closeModal);
  document.querySelector(".modal-close").addEventListener("click", closeModal);
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      closeModal();
    }
  });
}

function openInstallTargetModal(e) {
  const installTargetId = e.currentTarget.getAttribute("data-install-target");
  const modalContent = document
    .querySelector(
      `.install-target-template[data-install-target="${installTargetId}"]`,
    )
    .content.cloneNode(true);
  openModal(modalContent);
}

function toggleToolDetail(row) {
  const detail = row.nextElementSibling;
  if (detail && detail.classList.contains("tool-detail")) {
    row.classList.toggle("expanded");
    detail.classList.toggle("visible");
  }
}

function toolMatchesScope(row, tag) {
  if (!tag) return true;
  const tags = (row.getAttribute("data-tool-tags") || "")
    .split(",")
    .filter(Boolean);
  return tags.includes(tag);
}

function toolMatchesSearch(row, query) {
  if (!query) return true;
  const name = row.getAttribute("data-tool-name") || "";
  const desc = row.querySelector(".tool-desc");
  const descText = desc ? desc.textContent : "";
  return (
    name.toLowerCase().includes(query) || descText.toLowerCase().includes(query)
  );
}

// updateToolVisibility shows a tool only when it matches BOTH the active scope
// and the search query, so the two filters compose instead of clobbering each
// other's filtered-out class.
function updateToolVisibility() {
  const table = document.querySelector(".tools-table");
  if (!table) return;
  const rows = table.querySelectorAll(".tool-row");
  const q = toolSearchQuery.toLowerCase();
  let visibleCount = 0;
  for (const row of rows) {
    const match =
      toolMatchesScope(row, activeScopeTag) && toolMatchesSearch(row, q);
    row.classList.toggle("filtered-out", !match);
    const detail = row.nextElementSibling;
    if (detail && detail.classList.contains("tool-detail")) {
      detail.classList.toggle("filtered-out", !match);
      if (!match) {
        row.classList.remove("expanded");
        detail.classList.remove("visible");
      }
    }
    if (match) visibleCount++;
  }
  const noResults = table.querySelector(".tools-no-results");
  if (noResults)
    noResults.style.display = visibleCount === 0 ? "block" : "none";
}

// parseScopeVariants reads the server-emitted scope/connection map from the
// data-variants attribute. Returns null when the page has no scope filtering,
// leaving all scope behavior inert.
function parseScopeVariants() {
  const el = document.getElementById("scope-variants");
  if (!el) return null;
  try {
    return JSON.parse(el.dataset.variants);
  } catch (err) {
    console.error("Failed to parse scope variants:", err);
    return null;
  }
}

// applyScopeToTree swaps the connection strings for the given tag within root.
// root is the document for the always-visible elements, or a freshly cloned
// modal fragment (whose snippets start at the unfiltered default). Every value
// comes from the server-built variant map, so the client never encodes a URL.
function applyScopeToTree(root, tag) {
  if (!scopeVariants) return;
  const variant = scopeVariants[tag] || scopeVariants[""];
  if (!variant) return;

  root.querySelectorAll("[data-scope-url]").forEach((el) => {
    el.textContent = variant.url;
  });
  root.querySelectorAll("[data-scope-config]").forEach((el) => {
    el.textContent = variant.config;
  });
  root.querySelectorAll("[data-scope-cursor]").forEach((el) => {
    el.setAttribute("href", variant.cursor);
  });
  root.querySelectorAll("[data-scope-vscode]").forEach((el) => {
    el.setAttribute("href", variant.vscode);
  });
  // Command snippets embed the MCP URL inside a larger string; replace the
  // unfiltered default URL with the scoped URL (both server-built). On a fresh
  // modal clone the text starts at the default, so this single pass is correct.
  root.querySelectorAll("[data-scope-url-text]").forEach((el) => {
    if (defaultScopeUrl && variant.url !== defaultScopeUrl) {
      el.textContent = el.textContent.split(defaultScopeUrl).join(variant.url);
    }
  });
}

// updateToolCount sets the "Available Tools (N)" header to the number of tools
// in the active scope (ignoring the search box, which has its own empty state).
function updateToolCount() {
  const countEl = document.querySelector(".available-tools .tool-count");
  if (!countEl) return;
  const rows = document.querySelectorAll(".tools-table .tool-row");
  let count = 0;
  for (const row of rows) {
    if (toolMatchesScope(row, activeScopeTag)) count++;
  }
  countEl.textContent = "(" + count + ")";
}

function applyScope(tag) {
  if (!scopeVariants) return;
  if (!(tag in scopeVariants)) tag = "";
  activeScopeTag = tag;

  applyScopeToTree(document, tag);

  document.querySelectorAll(".scope-chip").forEach((chip) => {
    chip.classList.toggle(
      "active",
      (chip.getAttribute("data-scope-tag") || "") === tag,
    );
  });

  updateToolCount();
  updateToolVisibility();
}

// scopeTagFromLocation reads the selected scope from the ?tags= query parameter.
// The install page is single-select, so a shared multi-tag URL collapses to its
// first tag.
function scopeTagFromLocation() {
  const raw = new URLSearchParams(window.location.search).get("tags");
  if (!raw) return "";
  return raw.split(",")[0].trim();
}

function initializeScopes() {
  scopeVariants = parseScopeVariants();
  if (!scopeVariants) return;
  defaultScopeUrl = scopeVariants[""] ? scopeVariants[""].url : "";

  document.querySelectorAll(".scope-chip").forEach((chip) => {
    chip.addEventListener("click", (e) => {
      e.preventDefault();
      const tag = chip.getAttribute("data-scope-tag") || "";
      applyScope(tag);
      const nextURL = tag
        ? "?tags=" + encodeURIComponent(tag)
        : window.location.pathname;
      window.history.pushState({ scopeTag: tag }, "", nextURL);
    });
  });

  window.addEventListener("popstate", () => {
    applyScope(scopeTagFromLocation());
  });

  // Restore the scope from the URL so shared ?tags= links open pre-selected.
  applyScope(scopeTagFromLocation());
}

function copyContainerSnippet(e) {
  const containerEl = e.currentTarget;
  const text = containerEl.querySelector(".code-snippet").textContent;
  const copyBadge = containerEl.querySelector(".copy-button");

  navigator.clipboard
    .writeText(text)
    .then(() => {
      copyBadge.classList.replace("waiting", "success");
      setTimeout(() => {
        copyBadge.classList.replace("success", "waiting");
      }, 1000);
    })
    .catch((err) => {
      console.error("Failed to copy text: ", err);
    });
}

function registerCenterOffsetUpdaters(el) {
  let mouseX = 0;
  let mouseY = 0;
  let taskId = null;

  const updateOffset = () => {
    const rect = el.getBoundingClientRect();

    const xOffset = mouseX - rect.x - el.clientWidth / 2;
    const yOffset = mouseY - rect.y - el.clientHeight / 2;

    el.style.setProperty("--mouse-center-offset-x", xOffset);
    el.style.setProperty("--mouse-center-offset-y", yOffset);
    taskId = requestAnimationFrame(updateOffset);
  };

  el.addEventListener("mousemove", (e) => {
    mouseX = e.clientX;
    mouseY = e.clientY;
  });

  el.addEventListener("mouseenter", (e) => {
    updateOffset();
  });

  el.addEventListener("mouseleave", (e) => {
    cancelAnimationFrame(taskId);
  });
}

async function copyToClipboard(url, button, originalText) {
  try {
    await navigator.clipboard.writeText(url);
    button.innerHTML =
      '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg> Copied!';
    setTimeout(() => {
      button.innerHTML = originalText;
    }, 2000);
  } catch (err) {
    console.error("Failed to copy URL:", err);
  }
}

function initializeHandlers() {
  document
    .querySelector(".action-button")
    .addEventListener("click", togglePopover);

  document.querySelectorAll('[data-install-method="modal"]').forEach((el) => {
    el.addEventListener("click", openInstallTargetModal);
  });

  document.querySelectorAll(".code-container").forEach((el) => {
    el.addEventListener("click", copyContainerSnippet);
  });

  document.querySelectorAll(".tool-row").forEach((el) => {
    el.addEventListener("click", () => toggleToolDetail(el));
  });

  const toolsSearch = document.querySelector(".tools-search");
  if (toolsSearch) {
    toolsSearch.addEventListener("input", (e) => {
      toolSearchQuery = e.target.value;
      updateToolVisibility();
    });
  }

  initializeScopes();

  registerCenterOffsetUpdaters(document.querySelector(".gram-brand-badge"));

  document
    .getElementById("share-button")
    .addEventListener("click", async function () {
      const url = window.location.href;
      const button = this;
      const originalText = button.innerHTML;

      if (navigator.share) {
        try {
          await navigator.share({
            title: "{{ .MCPName }} - MCP Server",
            text: "Install {{ .MCPName }} MCP server by {{ .OrganizationName }}",
            url: url,
          });
        } catch (err) {
          if (err.name !== "AbortError") {
            await copyToClipboard(url, button, originalText);
          }
        }
      } else {
        await copyToClipboard(url, button, originalText);
      }
    });
}

document.addEventListener("DOMContentLoaded", () => {
  initializeHandlers();
});
