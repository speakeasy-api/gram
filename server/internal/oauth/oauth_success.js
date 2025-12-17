// OAuth success page redirect functionality
// When converting to .tmpl, the redirectURL will be injected via a data attribute

(function () {
  // Get redirect URL from data attribute or use mock for testing
  const redirectURL =
    document.body.getAttribute('data-redirect-url') || 'https://example.com';

  let secondsLeft = 5;
  const countdownElement = document.getElementById('countdown');
  const redirectMessage = document.getElementById('redirect-message');
  const redirectLink = document.getElementById('redirect-link');

  // Fallback redirect in case interval fails
  const fallbackTimeout = setTimeout(() => {
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
        redirectMessage.textContent = 'Redirecting now...';
      }
      window.location.href = redirectURL;
    }
  }, 1000);

  // Set the manual link to the same URL and clear timers on click
  if (redirectLink) {
    redirectLink.href = redirectURL;
    redirectLink.addEventListener('click', () => {
      clearInterval(countdownInterval);
      clearTimeout(fallbackTimeout);
    });
  }
})();
