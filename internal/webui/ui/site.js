(() => {
  "use strict";
  document.querySelectorAll("[data-copy]").forEach((button) =>
    button.addEventListener("click", async () => {
      const text = document.getElementById(button.dataset.copy)?.textContent;
      if (!text) return;
      try {
        await navigator.clipboard.writeText(text);
        button.textContent = "Copied";
      } catch {
        button.textContent = "Select to copy";
      }
      setTimeout(() => {
        button.textContent = "Copy";
      }, 2000);
    }),
  );
})();
