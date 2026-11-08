// Runs before the app bundle. Keep the keys and defaults in sync with src/prefs/storage.ts.
(function () {
  var root = document.documentElement;
  var mode = null;
  var lang = null;
  try {
    mode = localStorage.getItem("hx.mode");
    lang = localStorage.getItem("hx.lang");
  } catch {
    // Storage can be blocked (private mode, policy); fall back to defaults.
  }
  root.setAttribute("data-mode", mode === "light" ? "light" : "dark");
  root.setAttribute("lang", lang === "en" ? "en" : "zh-Hant-TW");
})();
