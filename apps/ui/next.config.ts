import type { NextConfig } from "next";

const isDev = process.env.NODE_ENV !== "production";
const apiURL = process.env.API_URL ?? "http://localhost:8080";

// Tailwind v4 runs as a Turbopack CSS loader (create-next-app 16 template).
const turbopack: NextConfig["turbopack"] = {
  rules: {
    "*.css": { loaders: ["@tailwindcss/turbopack"], as: "*.css" },
    "*.md": { condition: { query: "?raw" }, type: "text" },
  },
};

// Static export only at build; rewrites only in dev (Next does not support
// rewrites with `output: 'export'`).
const config: NextConfig = isDev
  ? {
      trailingSlash: true,
      // Otherwise /api/v1/session is 308-redirected to /api/v1/session/
      // before the rewrite runs; the API routes have no trailing slash.
      skipTrailingSlashRedirect: true,
      turbopack,
      async rewrites() {
        return [
          // Like nginx: every /p/<project>/… path is the one /p page.
          { source: "/p/:path+", destination: "/p/" },
          { source: "/api/:path*", destination: `${apiURL}/api/:path*` },
          { source: "/mcp/:project", destination: `${apiURL}/mcp/:project` },
        ];
      },
    }
  : { output: "export", trailingSlash: true, images: { unoptimized: true }, turbopack };

export default config;
