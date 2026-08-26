package apply

import (
	"fmt"
	"path/filepath"
)

const webRuntimePage = `import { getAPIConfiguration } from "@/lib/runtime-config";

export const dynamic = "force-dynamic";

export default function Home() {
  const apiConfiguration = getAPIConfiguration();

  return (
    <main
      data-smt-web-smoke="home"
      className="mx-auto flex min-h-screen max-w-3xl flex-col justify-center gap-6 px-6 py-16"
    >
      <p className="text-sm font-medium uppercase tracking-[0.2em] text-slate-500">
        SMT Web starter
      </p>
      <h1 className="text-4xl font-semibold tracking-tight text-slate-950">
        A small, ready-to-build Web surface.
      </h1>
      <p className="max-w-2xl text-lg leading-8 text-slate-600">
        This domain-neutral page verifies that the generated Next.js runtime is available without
        assuming an application domain or backend.
      </p>
      <p data-smt-web-api-config={apiConfiguration} className="text-sm text-slate-500">
        API configuration: {apiConfiguration}
      </p>
    </main>
  );
}
`

const webRuntimeHealthRoute = `export const dynamic = "force-dynamic";

export function GET() {
  return Response.json({ status: "ok" }, { headers: { "cache-control": "no-store" } });
}
`

const webRuntimeConfig = `export type APIConfiguration = "configured" | "not-configured";

export function getAPIConfiguration(): APIConfiguration {
  const value = process.env.API_BASE_URL?.trim();
  if (!value) {
    return "not-configured";
  }

  try {
    const url = new URL(value);
    if (!["http:", "https:"].includes(url.protocol)) {
      return "not-configured";
    }
    if (url.username || url.password) {
      return "not-configured";
    }
    return "configured";
  } catch {
    return "not-configured";
  }
}
`

const webRuntimeNextConfig = `import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
};

export default nextConfig;
`

const webRuntimeContainerfile = `FROM node:24.18.0-alpine AS dependencies
WORKDIR /app
RUN corepack enable pnpm
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts

FROM node:24.18.0-alpine AS builder
WORKDIR /app
RUN corepack enable pnpm
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm run build

FROM node:24.18.0-alpine AS runner
WORKDIR /app
RUN corepack enable pnpm
ENV NODE_ENV=production
ENV HOSTNAME=0.0.0.0
ENV PORT=3000
ENV NEXT_TELEMETRY_DISABLED=1
RUN addgroup -S nextjs && adduser -S nextjs -G nextjs
COPY --from=builder --chown=nextjs:nextjs /app/public ./public
COPY --from=dependencies --chown=nextjs:nextjs /app/node_modules ./node_modules
COPY --from=builder --chown=nextjs:nextjs /app/.next ./.next
COPY --from=builder --chown=nextjs:nextjs /app/next.config.ts ./next.config.ts
COPY --from=builder --chown=nextjs:nextjs /app/package.json ./package.json
USER nextjs
EXPOSE 3000
STOPSIGNAL SIGTERM
CMD ["pnpm", "start"]
`

func webRuntimeFiles() map[string]string {
	return map[string]string{
		filepath.Join("app", "page.tsx"):            webRuntimePage,
		filepath.Join("app", "healthz", "route.ts"): webRuntimeHealthRoute,
		filepath.Join("lib", "runtime-config.ts"):   webRuntimeConfig,
		"next.config.ts":                            webRuntimeNextConfig,
		"Containerfile":                             webRuntimeContainerfile,
		filepath.Join("public", ".gitkeep"):         "",
	}
}

func writeWebRuntimeFiles(directory string) error {
	for relative, contents := range webRuntimeFiles() {
		if err := writeFile(filepath.Join(directory, relative), contents); err != nil {
			return fmt.Errorf("write %s: %w", relative, err)
		}
	}
	return nil
}
