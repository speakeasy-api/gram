import { zip } from "./fflate.js";

function downloadDxtHandler(event) {
  const manifestElement = document.getElementById("manifest-json-blob");
  if (manifestElement) {
    const manifestContent = manifestElement.textContent;

    const files = {
      "manifest.json": new TextEncoder().encode(manifestContent),
    };

    zip(files, (err, data) => {
      if (err) {
        console.error("Error creating zip:", err);
        return;
      }

      const blob = new Blob([data], { type: "application/zip" });
      const url = URL.createObjectURL(blob);
      const ourEl = document.querySelector(".install-targets");
      const a = document.createElement("a");
      a.href = url;
      a.download = "manifest.dxt";
      ourEl.appendChild(a);
      a.click();
      ourEl.removeChild(a);
      URL.revokeObjectURL(url);
    });
  }
}

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

function openModal(childContent) {
  const template = document.querySelector("#modal-template");
  const modalElement = template.content.cloneNode(true);
  modalElement.querySelector(".content-slot").appendChild(childContent);
  document.body.appendChild(modalElement);
  const backdrop = document.querySelector(".modal-backdrop");
  backdrop.addEventListener("scroll", (e) => e.stopPropagation());
  backdrop.addEventListener("click", () => {
    const modal = document.querySelector(".modal");
    modal.remove();
    backdrop.remove();
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

function toggleToolVisibility() {
  const hiddenTools = document.querySelectorAll(".tool-name.hidden");
  if (hiddenTools.length > 0) {
    hiddenTools.forEach((el) => el.classList.remove("hidden"));
  } else {
    document
      .querySelectorAll(".tool-name:nth-child(n + 11)")
      .forEach((el) => el.classList.add("hidden"));
  }
}

function copyClaudeCodeSnippet() {
  const installElement = document.getElementById("claude-code-script");
  if (installElement) {
    const text = installElement.textContent;
    navigator.clipboard
      .writeText(text)
      .then(() => {
        const button = e.target.classList.contains("quick-action")
          ? e.target
          : e.target.closest(".quick-action");
        const originalText = button.textContent;
        button.textContent = "Copied!";
        setTimeout(() => {
          button.textContent = originalText;
        }, 2000);
      })
      .catch((err) => {
        console.error("Failed to copy text: ", err);
      });
  }
}

function copyContainerSnippet(e) {
  const codeEl = e.target.parentElement.querySelector('pre');
  console.log(e)
  const text = codeEl.textContent;
  navigator.clipboard
    .writeText(text)
    .then(() => {
      const button = e.target.classList.contains("quick-action")
        ? e.target
        : e.target.closest(".quick-action");
      const originalText = button.textContent;
      button.textContent = "Copied!";
      setTimeout(() => {
        button.textContent = originalText;
      }, 2000);
    })
    .catch((err) => {
      console.error("Failed to copy text: ", err);
    });
}

function initializeHandlers() {
  document
    .querySelector(".action-button")
    .addEventListener("click", togglePopover);

  document.querySelectorAll('[data-install-method="modal"]').forEach((el) => {
    el.addEventListener("click", openInstallTargetModal);
  });

  document.querySelectorAll(".code-container .copy-button").forEach((el) => {
    el.addEventListener("click", copyContainerSnippet);
  });

  document
    .querySelector('.install-target[data-install-target="claude-desktop"]')
    .addEventListener("click", (e) => {
      e.stopPropagation();
      downloadDxtHandler(e);
    });
}

document.addEventListener("DOMContentLoaded", () => {
  initializeHandlers();
});
