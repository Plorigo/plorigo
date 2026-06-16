package builder

import (
	"strings"
	"text/template"
)

// This file holds the per-runtime Dockerfile templates and the rendering helpers. They are
// deliberately small and previewable: the dashboard shows the exact text below before a
// deploy, and the agent writes it verbatim and builds it — what you see is what runs.

// dockerfileParams is the data a template needs. Values come only from the detected package
// manager and fixed script names, so none can contain quotes — the JSON-array CMD stays valid.
type dockerfileParams struct {
	BaseImage      string
	EnableCorepack bool
	InstallCmd     string
	BuildCmd       string // may be empty (a plain Node app without a build step)
	StartCmd       string // server runtimes only
	Port           int32
	NodeMajor      string // Vite's static-serve final stage
	OutputDir      string // Vite's build output directory
}

// serverTemplate runs a long-lived Node process (Next.js, plain Node). Next.js and well-behaved
// Node apps read $PORT, which we pin to match EXPOSE.
var serverTemplate = template.Must(template.New("server").Parse(`FROM {{.BaseImage}}
WORKDIR /app
{{- if .EnableCorepack}}
RUN corepack enable
{{- end}}
COPY . .
RUN {{.InstallCmd}}
{{- if .BuildCmd}}
RUN {{.BuildCmd}}
{{- end}}
ENV NODE_ENV=production
ENV PORT={{.Port}}
EXPOSE {{.Port}}
CMD ["sh", "-c", "{{.StartCmd}}"]
`))

// viteTemplate builds a Vite SPA, then serves the static output from a clean Node stage with a
// pinned static file server. SSR Vite is out of scope for this slice.
var viteTemplate = template.Must(template.New("vite").Parse(`FROM {{.BaseImage}} AS build
WORKDIR /app
{{- if .EnableCorepack}}
RUN corepack enable
{{- end}}
COPY . .
RUN {{.InstallCmd}}
RUN {{.BuildCmd}}

FROM node:{{.NodeMajor}}-bookworm-slim
WORKDIR /app
RUN npm install -g serve@14
COPY --from=build /app/{{.OutputDir}} ./{{.OutputDir}}
ENV NODE_ENV=production
ENV PORT={{.Port}}
EXPOSE {{.Port}}
CMD ["sh", "-c", "serve -s {{.OutputDir}} -l {{.Port}}"]
`))

func render(t *template.Template, p dockerfileParams) string {
	var b strings.Builder
	// The templates above are static and the params quote-free, so Execute cannot fail here.
	_ = t.Execute(&b, p)
	return b.String()
}

// baseImage is the build image for a package manager. Bun ships its own image; npm/pnpm/yarn
// run on the pinned Node LTS slim image (which still bundles corepack — node:25+ dropped it).
func baseImage(pm, node string) string {
	if pm == "bun" {
		return "oven/bun:1"
	}
	return "node:" + node + "-bookworm-slim"
}

func enableCorepack(pm string) bool { return pm == "pnpm" || pm == "yarn" }

// installCmd installs dependencies. npm prefers a clean, lockfile-exact install when a lockfile
// is present, but falls back to a normal install if the lockfile is out of sync with
// package.json — `npm ci` hard-fails on any drift, which is a common, surprising failure for a
// Dockerfile the user didn't write. The others install normally (corepack resolves the pinned
// pnpm/yarn version).
func installCmd(pm string, hasLock bool) string {
	switch pm {
	case "pnpm":
		return "pnpm install"
	case "yarn":
		return "yarn install"
	case "bun":
		return "bun install"
	default:
		if hasLock {
			return "npm ci || npm install"
		}
		return "npm install"
	}
}

// runScript is how a package manager runs a package.json script.
func runScript(pm, name string) string {
	switch pm {
	case "yarn":
		return "yarn " + name
	case "bun":
		return "bun run " + name
	case "pnpm":
		return "pnpm run " + name
	default:
		return "npm run " + name
	}
}

// runBinary is how a package manager runs a locally-installed binary, for when a framework has
// no matching script (e.g. a Next.js app missing a "build" script still has the `next` binary).
func runBinary(pm, argv string) string {
	switch pm {
	case "yarn":
		return "yarn " + argv
	case "bun":
		return "bun x " + argv
	case "pnpm":
		return "pnpm exec " + argv
	default:
		return "npm exec -- " + argv
	}
}

func renderNext(pm string, hasLock bool, node string, pkg packageJSON) Plan {
	build := runScript(pm, "build")
	if pkg.script("build") == "" {
		build = runBinary(pm, "next build")
	}
	start := runScript(pm, "start")
	if pkg.script("start") == "" {
		start = runBinary(pm, "next start")
	}
	return Plan{
		Status:         StatusDetected,
		Runtime:        RuntimeNext,
		PackageManager: pm,
		NodeVersion:    node,
		BuildCommand:   build,
		StartCommand:   start,
		Port:           defaultPort,
		Dockerfile: render(serverTemplate, dockerfileParams{
			BaseImage:      baseImage(pm, node),
			EnableCorepack: enableCorepack(pm),
			InstallCmd:     installCmd(pm, hasLock),
			BuildCmd:       build,
			StartCmd:       start,
			Port:           defaultPort,
		}),
	}
}

func renderNode(pm string, hasLock bool, node string, pkg packageJSON) Plan {
	build := "" // a build step only if the project declares one
	if pkg.script("build") != "" {
		build = runScript(pm, "build")
	}
	start := runScript(pm, "start") // guaranteed present by the caller
	return Plan{
		Status:         StatusDetected,
		Runtime:        RuntimeNode,
		PackageManager: pm,
		NodeVersion:    node,
		BuildCommand:   build,
		StartCommand:   start,
		Port:           defaultPort,
		Dockerfile: render(serverTemplate, dockerfileParams{
			BaseImage:      baseImage(pm, node),
			EnableCorepack: enableCorepack(pm),
			InstallCmd:     installCmd(pm, hasLock),
			BuildCmd:       build,
			StartCmd:       start,
			Port:           defaultPort,
		}),
	}
}

func renderVite(pm string, hasLock bool, node string, pkg packageJSON) Plan {
	build := runScript(pm, "build")
	if pkg.script("build") == "" {
		build = runBinary(pm, "vite build")
	}
	const outputDir = "dist"
	return Plan{
		Status:         StatusDetected,
		Runtime:        RuntimeVite,
		PackageManager: pm,
		NodeVersion:    node,
		BuildCommand:   build,
		StartCommand:   "serve -s " + outputDir,
		Port:           defaultPort,
		Dockerfile: render(viteTemplate, dockerfileParams{
			BaseImage:      baseImage(pm, node),
			EnableCorepack: enableCorepack(pm),
			InstallCmd:     installCmd(pm, hasLock),
			BuildCmd:       build,
			Port:           defaultPort,
			NodeMajor:      node,
			OutputDir:      outputDir,
		}),
	}
}
