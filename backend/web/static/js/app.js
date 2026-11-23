(() => {
  document.addEventListener(
    "submit",
    (event) => {
      const form = event.target;
      if (!(form instanceof HTMLFormElement)) {
        return;
      }
      const message = form.getAttribute("data-confirm");
      if (message && !window.confirm(message)) {
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    },
    true,
  );

  // The header disclosures work without JavaScript; this only adds the light
  // dismiss and Escape handling a native details element does not provide.
  const openHeaderDisclosures = () =>
    document.querySelectorAll("header details[open]");

  document.addEventListener("click", (event) => {
    for (const details of openHeaderDisclosures()) {
      if (!details.contains(event.target)) {
        details.open = false;
      }
    }
  });

  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape") {
      return;
    }
    for (const details of openHeaderDisclosures()) {
      details.open = false;
      const summary = details.querySelector("summary");
      if (summary) {
        summary.focus();
      }
    }
  });
})();
