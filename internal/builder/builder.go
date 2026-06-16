// Package builder inspects a repository's files and, for a supported framework, produces a
// build plan: the runtime, package manager, Node version, build/start commands, port, and a
// rendered Dockerfile. It is the engine behind Plorigo's Dockerfile-less builds.
//
// It is a NEUTRAL, dependency-free package (stdlib only) so BOTH the server agent (over a
// local clone, via OSFiles) and the control plane (over the GitHub contents API) run the same
// detection — what the dashboard previews is exactly what the agent builds. The detection
// rules and Dockerfile templates live HERE as data, not in the docs (see
// docs/architecture/deployment-engine.md): this package IS the catalogue.
package builder

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Files is the minimal read-only view of a repository the detector needs. ReadFile returns
// (nil, false, nil) when the file is absent — a missing file is not an error. Paths are
// always repo-root-relative and use forward slashes (e.g. "package.json").
type Files interface {
	ReadFile(path string) (data []byte, ok bool, err error)
}

// Status is the outcome of detection.
type Status string

const (
	// StatusDetected means a supported framework was identified and a Dockerfile rendered.
	StatusDetected Status = "detected"
	// StatusHasDockerfile means the repo already has its own Dockerfile; the existing build path
	// handles it and the detector stays out of the way.
	StatusHasDockerfile Status = "dockerfile"
	// StatusUnsupported means no Dockerfile and no supported framework; NextSteps says what to do.
	StatusUnsupported Status = "unsupported"
)

// Runtime machine values (the friendly label comes from RuntimeLabel).
const (
	RuntimeNext = "nextjs"
	RuntimeVite = "vite"
	RuntimeNode = "nodejs"
)

// Plan is the result of Detect. For StatusDetected it is fully populated and Dockerfile holds
// the rendered text the agent writes and builds; for StatusUnsupported only NextSteps is set.
type Plan struct {
	Status         Status
	Runtime        string // RuntimeNext | RuntimeVite | RuntimeNode (empty unless Detected)
	PackageManager string // "npm" | "pnpm" | "yarn" | "bun"
	NodeVersion    string // major only, e.g. "22"
	BuildCommand   string // human-readable; empty when there's no build step
	StartCommand   string // human-readable
	Port           int32
	Dockerfile     string // rendered; empty unless Detected
	NextSteps      string // plain-English guidance; set when Unsupported
}

// RuntimeLabel is the human-friendly name for the detected runtime.
func (p Plan) RuntimeLabel() string {
	switch p.Runtime {
	case RuntimeNext:
		return "Next.js"
	case RuntimeVite:
		return "Vite"
	case RuntimeNode:
		return "Node.js"
	default:
		return p.Runtime
	}
}

// defaultPort is the port a generated container listens on (and EXPOSEs) across all three
// runtimes: Next.js honors $PORT, `serve` is told -l, and a plain Node app is expected to read
// $PORT. The agent still falls back to the image's EXPOSE when the request port is 0.
const defaultPort int32 = 3000

// defaultNodeMajor is the Node version used when the repo pins none (current LTS).
const defaultNodeMajor = "22"

const (
	stepsNoPackageJSON = "No Dockerfile and no package.json were found at the repository root. " +
		"Plorigo builds Node, Vite, and Next.js apps automatically, or any app with a Dockerfile. " +
		"Add a Dockerfile, or deploy from the root of a Node project."
	stepsBadPackageJSON = "package.json could not be parsed as JSON. Fix it, or add a Dockerfile to control the build yourself."
	stepsNodeNoStart    = "This looks like a Node project, but it has no \"start\" script and isn't a Next.js or Vite app. " +
		"Add a \"start\" script to package.json (for example \"node server.js\"), or add a Dockerfile."
)

// Detect inspects f and returns a build plan. It returns an error only on a real read failure
// (e.g. a network error from the control-plane file accessor); an unsupported repo is a normal
// Plan with StatusUnsupported, not an error.
func Detect(f Files) (Plan, error) {
	// 1. A repo Dockerfile always wins (matches the build-priority order); leave it alone.
	if _, ok, err := f.ReadFile("Dockerfile"); err != nil {
		return Plan{}, err
	} else if ok {
		return Plan{Status: StatusHasDockerfile}, nil
	}

	// 2. Every framework we support is a Node project rooted at package.json.
	raw, ok, err := f.ReadFile("package.json")
	if err != nil {
		return Plan{}, err
	}
	if !ok {
		return Plan{Status: StatusUnsupported, NextSteps: stepsNoPackageJSON}, nil
	}
	var pkg packageJSON
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return Plan{Status: StatusUnsupported, NextSteps: stepsBadPackageJSON}, nil
	}

	pm, hasLock, err := detectPackageManager(f)
	if err != nil {
		return Plan{}, err
	}
	nvmrc, _, err := f.ReadFile(".nvmrc")
	if err != nil {
		return Plan{}, err
	}
	node := nodeMajor(string(nvmrc), pkg.engineNode())

	// 3. Classify. Next.js before Vite before a plain Node server.
	switch {
	case pkg.hasDep("next"):
		return renderNext(pm, hasLock, node, pkg), nil
	case pkg.hasDep("vite"):
		return renderVite(pm, hasLock, node, pkg), nil
	case pkg.script("start") != "":
		return renderNode(pm, hasLock, node, pkg), nil
	default:
		return Plan{Status: StatusUnsupported, NextSteps: stepsNodeNoStart}, nil
	}
}

// packageJSON is the slice of package.json the rules read.
type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         struct {
		Node string `json:"node"`
	} `json:"engines"`
}

func (p packageJSON) hasDep(name string) bool {
	if _, ok := p.Dependencies[name]; ok {
		return true
	}
	_, ok := p.DevDependencies[name]
	return ok
}

func (p packageJSON) script(name string) string { return strings.TrimSpace(p.Scripts[name]) }

func (p packageJSON) engineNode() string { return p.Engines.Node }

// detectPackageManager picks the package manager from the lockfile present at the repo root,
// defaulting to npm. hasLock reports whether a recognized lockfile was found (npm needs one
// for `npm ci`).
func detectPackageManager(f Files) (pm string, hasLock bool, err error) {
	for _, c := range []struct {
		file string
		pm   string
	}{
		{"pnpm-lock.yaml", "pnpm"},
		{"yarn.lock", "yarn"},
		{"bun.lockb", "bun"},
		{"package-lock.json", "npm"},
		{"npm-shrinkwrap.json", "npm"},
	} {
		_, ok, rerr := f.ReadFile(c.file)
		if rerr != nil {
			return "", false, rerr
		}
		if ok {
			return c.pm, true, nil
		}
	}
	return "npm", false, nil
}

var digits = regexp.MustCompile(`\d+`)

// nodeMajor resolves a Node major version from .nvmrc (preferred) then package.json engines,
// falling back to the default LTS. Anything outside a sane range (so the base image tag stays
// valid) falls back too.
func nodeMajor(nvmrc, engines string) string {
	for _, src := range []string{nvmrc, engines} {
		if m := digits.FindString(strings.TrimSpace(src)); m != "" {
			if n, err := strconv.Atoi(m); err == nil && n >= 18 && n <= 24 {
				return strconv.Itoa(n)
			}
		}
	}
	return defaultNodeMajor
}

// OSFiles is the agent-side Files: it reads from a local clone rooted at dir. Paths are
// repo-relative, cleaned and contained within dir so a crafted name can't escape the tree.
func OSFiles(dir string) Files { return osFiles{root: dir} }

type osFiles struct{ root string }

func (o osFiles) ReadFile(p string) ([]byte, bool, error) {
	clean := path.Clean("/" + p) // anchor, collapse any .. — result always starts with "/"
	full := filepath.Join(o.root, filepath.FromSlash(clean))
	b, err := os.ReadFile(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}
