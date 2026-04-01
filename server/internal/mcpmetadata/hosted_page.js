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
