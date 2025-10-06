export function initCopyMarkdown() {
  document.addEventListener('keydown', async (event) => {
    // Check for Cmd+C on Mac or Ctrl+C on Windows/Linux
    const isCopyShortcut = (event.metaKey || event.ctrlKey) && event.key === 'c';
    
    if (!isCopyShortcut) return;
    
    // Check if user has selected text (don't override normal copy behavior)
    const selection = window.getSelection();
    if (selection && selection.toString().trim()) {
      return; // Let the default copy behavior handle selected text
    }
    
    // Prevent default copy behavior
    event.preventDefault();
    
    // Extract the page ID from the current URL
    const pathname = window.location.pathname;
    const pageId = pathname === '/' ? 'index' : pathname.slice(1);
    
    try {
      // Fetch the markdown content
      const response = await fetch(`${pageId}.md`);
      
      if (!response.ok) {
        console.error('Failed to fetch markdown content');
        return;
      }
      
      const markdown = await response.text();
      
      // Copy to clipboard
      await navigator.clipboard.writeText(markdown);
      
      // Show visual feedback
      showCopyFeedback();
    } catch (error) {
      console.error('Error copying markdown:', error);
    }
  });
}

function showCopyFeedback() {
  // Create a temporary toast notification
  const toast = document.createElement('div');
  toast.textContent = 'Markdown copied to clipboard!';
  toast.style.cssText = `
    position: fixed;
    bottom: 20px;
    right: 20px;
    background: #059669;
    color: white;
    padding: 12px 24px;
    border-radius: 8px;
    font-size: 14px;
    z-index: 9999;
    animation: slideIn 0.3s ease-out;
  `;
  
  // Add animation styles
  const style = document.createElement('style');
  style.textContent = `
    @keyframes slideIn {
      from {
        transform: translateY(100%);
        opacity: 0;
      }
      to {
        transform: translateY(0);
        opacity: 1;
      }
    }
  `;
  document.head.appendChild(style);
  
  document.body.appendChild(toast);
  
  // Remove toast after 3 seconds
  setTimeout(() => {
    toast.style.animation = 'slideIn 0.3s ease-out reverse';
    setTimeout(() => {
      toast.remove();
      style.remove();
    }, 300);
  }, 3000);
}