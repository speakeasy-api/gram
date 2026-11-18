// The authorization error page's only script: the technical details copy
// button. It must never navigate; the person chooses whether to return to the
// client. oauthtest allowlists this exact text, so any change here is reviewed.
{
  const button = document.querySelector("[data-copy-details]");
  const copyIcon = button.querySelector("[data-copy-icon]");
  const copiedIcon = button.querySelector("[data-copied-icon]");
  const status = document.querySelector("[data-copy-status]");
  let reset;
  button.hidden = false;
  button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(button.dataset.copyText);
    } catch {
      status.textContent =
        "Couldn't copy. Select the details and copy them instead.";
      return;
    }
    copyIcon.hidden = true;
    copiedIcon.hidden = false;
    status.textContent = "Technical details copied";
    clearTimeout(reset);
    reset = setTimeout(() => {
      copyIcon.hidden = false;
      copiedIcon.hidden = true;
      status.textContent = "";
    }, 1000);
  });
}
