import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./styles/app.css";
import { App } from "./app/App";
import { initI18n } from "./i18n";
import { savedMode, setMode } from "./prefs/mode";

initI18n();
setMode(savedMode());

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
