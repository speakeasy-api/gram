(function () {
  const redirectURL = document.body.getAttribute("data-redirect-url");
  const countdownElement = document.getElementById("countdown");
  const redirectMessage = document.getElementById("redirect-message");
  const redirectLink = document.getElementById("redirect-link");

  let secondsLeft = 5;

  const fallbackTimeout = setTimeout(() => {
    if (redirectMessage) {
      redirectMessage.textContent = "It is ok to close this page.";
    }
    window.location.href = redirectURL;
  }, 6000);

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

  if (redirectLink) {
    redirectLink.addEventListener("click", () => {
      clearInterval(countdownInterval);
      clearTimeout(fallbackTimeout);
    });
  }
})();
