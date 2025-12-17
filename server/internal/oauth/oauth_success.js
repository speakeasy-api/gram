// OAuth success page redirect functionality
// When converting to .tmpl, the redirectURL will be injected via a data attribute

(function () {
  // Get redirect URL from data attribute or use mock for testing
  const redirectURL = document.body.getAttribute("data-redirect-url");

  let secondsLeft = 5;
  const countdownElement = document.getElementById("countdown");
  const redirectMessage = document.getElementById("redirect-message");
  const redirectLink = document.getElementById("redirect-link");

  const postRedirectMessage = "It is ok to close this page.";

  // Fallback redirect in case interval fails
  const fallbackTimeout = setTimeout(() => {
    if (redirectMessage) {
      redirectMessage.textContent = postRedirectMessage;
    }
    window.location.href = redirectURL;
  }, 6000);

  // Update countdown every second
  const countdownInterval = setInterval(() => {
    secondsLeft--;
    if (countdownElement) {
      countdownElement.textContent = secondsLeft;
    }

    if (secondsLeft <= 0) {
      clearInterval(countdownInterval);
      clearTimeout(fallbackTimeout);
      if (redirectMessage) {
        redirectMessage.textContent = "It is ok to close this page.";
      }
      window.location.href = redirectURL;
    }
  }, 1000);

  // Set the manual link to the same URL and clear timers on click
  if (redirectLink) {
    redirectLink.addEventListener("click", () => {
      clearInterval(countdownInterval);
      clearTimeout(fallbackTimeout);
    });
  }
})();
