// The organization gateway page's only script: the copy buttons. It must
// never navigate.
{
  const status = document.querySelector("[data-copy-status]");
  for (const button of document.querySelectorAll("[data-copy]")) {
    const copyIcon = button.querySelector("[data-copy-icon]");
    const copiedIcon = button.querySelector("[data-copied-icon]");
    let reset;
    button.hidden = false;
    button.addEventListener("click", async () => {
      try {
        await navigator.clipboard.writeText(button.dataset.copyText);
      } catch {
        status.textContent =
          "Couldn't copy. Select the text and copy it instead.";
        return;
      }
      copyIcon.hidden = true;
      copiedIcon.hidden = false;
      status.textContent = "Copied";
      clearTimeout(reset);
      reset = setTimeout(() => {
        copyIcon.hidden = false;
        copiedIcon.hidden = true;
        status.textContent = "";
      }, 1000);
    });
  }
}
