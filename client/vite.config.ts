import { createHash } from "node:crypto";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig, type Plugin } from "vite";

const publicShellFiles = [
  "/manifest.webmanifest",
  "/macroterminal.png",
  "/icons/icon-192.png",
  "/icons/icon-512.png",
  "/icons/maskable-512.png",
];

function byteLength(source: string | Uint8Array | undefined): number {
  if (source === undefined) return 0;
  return typeof source === "string" ? source.length : source.byteLength;
}

function serviceWorker(): Plugin {
  return {
    name: "macro-terminal-service-worker",
    apply: "build",
    generateBundle(_options, bundle) {
      const entries = Object.entries(bundle).filter(([file]) => file !== "sw.js");
      const buildId = createHash("sha256")
        .update(
          entries
            .map(([file, output]) =>
              output.type === "chunk"
                ? `${file}:${output.code.length}`
                : `${file}:${byteLength(output.source)}`,
            )
            .join("|"),
        )
        .digest("hex")
        .slice(0, 16);
      const precache = [
        "/",
        ...publicShellFiles,
        ...entries.map(([file]) => `/${file}`),
      ].sort();
      const source = serviceWorkerSource(buildId, precache);
      this.emitFile({ type: "asset", fileName: "sw.js", source });
    },
  };
}

function serviceWorkerSource(buildId: string, precache: string[]): string {
  return `const CACHE_NAME = "macro-terminal-shell-${buildId}";
const PRECACHE = ${JSON.stringify(precache, null, 2)};

self.addEventListener("install", (event) => {
  event.waitUntil(caches.open(CACHE_NAME).then((cache) => cache.addAll(PRECACHE)));
});

self.addEventListener("message", (event) => {
  if (event.data && event.data.type === "skip-waiting") {
    self.skipWaiting();
  }
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    caches
      .keys()
      .then((keys) =>
        Promise.all(
          keys
            .filter((key) => key.startsWith("macro-terminal-shell-") && key !== CACHE_NAME)
            .map((key) => caches.delete(key)),
        ),
      )
      .then(() => self.clients.claim()),
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;
  if (request.method !== "GET") return;
  const url = new URL(request.url);
  if (url.origin !== self.location.origin) return;
  if (url.pathname.startsWith("/api/") || url.pathname.startsWith("/auth/")) return;
  if (request.mode === "navigate") {
    event.respondWith(
      fetch(request).catch(() =>
        caches
          .match("/")
          .then(
            (cached) =>
              cached ||
              new Response("Offline", { status: 503, headers: { "Content-Type": "text/plain" } }),
          ),
      ),
    );
    return;
  }
  event.respondWith(caches.match(request).then((cached) => cached || fetch(request)));
});
`;
}

export default defineConfig({
  plugins: [react(), tailwindcss(), serviceWorker()],
  server: {
    port: 5173,
  },
  preview: {
    port: 5173,
  },
});
