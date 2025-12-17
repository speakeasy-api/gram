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

  // Set the manual link to the same URL
  if (redirectLink) {
    redirectLink.href = redirectURL;
  }

  // Update countdown every second
  const countdownInterval = setInterval(() => {
    secondsLeft--;
    if (countdownElement) {
      countdownElement.textContent = secondsLeft;
    }

    if (secondsLeft <= 0) {
      clearInterval(countdownInterval);
      if (redirectMessage) {
        redirectMessage.textContent = 'Redirecting now...';
      }
      window.location.href = redirectURL;
    }
  }, 1000);

  // Fallback redirect in case interval fails
  setTimeout(() => {
    window.location.href = redirectURL;
  }, 5000);
})();
