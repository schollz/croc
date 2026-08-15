import React from "react";
import ReactDOM from "react-dom/client";
import "@fontsource-variable/jetbrains-mono";
import "driver.js/dist/driver.css";
import "./styles.css";
import { App } from "./App";
import { Blog } from "./Blog";
import { preloadWasm } from "./wasm/client";

const blogPath = window.location.pathname.replace(/\/$/, "") || "/";
const isBlogRoute = blogPath === "/blog" || blogPath.startsWith("/blog/");
const blogSlug = blogPath.startsWith("/blog/")
  ? blogPath.slice("/blog/".length)
  : undefined;

if (!isBlogRoute) preloadWasm();

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    {isBlogRoute ? <Blog slug={blogSlug} /> : <App />}
  </React.StrictMode>,
);
