(() => {
  const osTheme = window.matchMedia
    ? window.matchMedia("(prefers-color-scheme: dark)")
    : { matches: false };

  function normalizeThemeMode(mode) {
    return mode === "light" || mode === "dark" || mode === "system" ? mode : "system";
  }

  function storedThemeMode() {
    try {
      return normalizeThemeMode(
        localStorage.getItem("hby-control-theme-mode") ||
          localStorage.getItem("hby-control-theme") ||
          "system",
      );
    } catch {
      return "system";
    }
  }

  function resolvedTheme(mode) {
    return mode === "system" ? (osTheme.matches ? "dark" : "light") : mode;
  }

  function syncThemeControls(mode) {
    document.querySelectorAll('input[name="themeMode"]').forEach((input) => {
      input.checked = input.value === mode;
    });
  }

  function applyThemeMode(mode) {
    mode = normalizeThemeMode(mode);
    document.documentElement.dataset.themeMode = mode;
    document.documentElement.dataset.theme = resolvedTheme(mode);
    syncThemeControls(mode);
  }

  function saveThemeMode(mode) {
    mode = normalizeThemeMode(mode);
    try {
      localStorage.setItem("hby-control-theme-mode", mode);
      localStorage.removeItem("hby-control-theme");
    } catch {}
    applyThemeMode(mode);
  }

  function initControls() {
    applyThemeMode(storedThemeMode());
    document.querySelectorAll('input[name="themeMode"]').forEach((input) => {
      input.onchange = (e) => {
        if (e.target.checked) saveThemeMode(e.target.value);
      };
    });
  }

  function syncSystemTheme() {
    if (document.documentElement.dataset.themeMode === "system") {
      applyThemeMode("system");
    }
  }

  if (osTheme.addEventListener) {
    osTheme.addEventListener("change", syncSystemTheme);
  } else if (osTheme.addListener) {
    osTheme.addListener(syncSystemTheme);
  }

  window.hbyTheme = { applyThemeMode, initControls, saveThemeMode, storedThemeMode };
  applyThemeMode(storedThemeMode());
})();
