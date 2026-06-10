try {
  const mode = localStorage.getItem("hby-control-theme-mode") || localStorage.getItem("hby-control-theme") || "system";
  const media = matchMedia("(prefers-color-scheme: dark)");
  document.documentElement.dataset.themeMode = mode;
  document.documentElement.dataset.theme = mode === "system" ? (media.matches ? "dark" : "light") : mode;
} catch {}
