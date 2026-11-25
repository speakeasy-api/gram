// Served as an external file, not inlined into oauth_page.html, because the
// ingress CSP forbids inline scripts.

for (const form of document.querySelectorAll("form[data-single-submit]")) {
  form.addEventListener(
    "submit",
    () => {
      const submittedButton = form.querySelector("button");
      if (submittedButton?.value) {
        const action = document.createElement("input");
        action.type = "hidden";
        action.name = "action";
        action.value = submittedButton.value;
        form.append(action);
      }
      for (const button of document.querySelectorAll(
        "form[data-single-submit] button",
      )) {
        button.disabled = true;
      }
      if (submittedButton) submittedButton.textContent = "Connecting…";
    },
    { once: true },
  );
}

if (document.body.hasAttribute("data-auto-close")) {
  window.close();
}
