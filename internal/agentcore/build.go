package agentcore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/plorigo/plorigo/internal/builder"
)

// build.go is the agent's build-from-Git path: clone a repo and build it into a local image the
// deploy loop then runs. A public repo is cloned anonymously; a private repo (backed by a GitHub
// App installation) is cloned with the short-lived installation token the control plane minted and
// put in the job. When the repo ships its own Dockerfile we build that; when it doesn't, we detect
// a supported framework (Node/Vite/Next.js) with internal/builder and build a generated Dockerfile.
// See docs/architecture/agent.md and deployment-engine.md.

// clone does a shallow checkout of cloneURL at gitRef into dir and returns the exact commit SHA it
// landed on. gitRef is treated as a branch (the value sources store); an empty ref clones the
// remote's default branch. credential is empty for a public repo (anonymous clone) or a GitHub App
// installation token for a private repo — applied as the password of an x-access-token basic-auth,
// kept OUT of the URL so it can never leak into a log line or the stored clone URL, and never emitted.
func (d *dockerClient) clone(ctx context.Context, cloneURL, gitRef, credential, dir string, emit func(string)) (string, error) {
	opts := &git.CloneOptions{
		URL:          cloneURL,
		Depth:        1,
		SingleBranch: true,
		Tags:         git.NoTags,
	}
	if credential != "" {
		opts.Auth = &githttp.BasicAuth{Username: "x-access-token", Password: credential}
	}
	if ref := strings.TrimSpace(gitRef); ref != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		emit("cloning " + cloneURL + " (branch " + ref + ")")
	} else {
		emit("cloning " + cloneURL + " (default branch)")
	}
	repo, err := git.PlainCloneContext(ctx, dir, false, opts)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("resolve checked-out commit: %w", err)
	}
	sha := head.Hash().String()
	emit("checked out " + shortID(sha))
	return sha, nil
}

// build builds dir into the local image tag using BuildKit (DOCKER_BUILDKIT=1), streaming the
// build output through emit. It prefers the repo's own Dockerfile; when there is none it
// detects a supported framework (Node/Vite/Next.js) and writes a generated Dockerfile to build
// instead. An unsupported repo is reported as clear, plain-English next steps rather than a raw
// builder error.
func (d *dockerClient) build(ctx context.Context, dir, tag string, emit func(string)) error {
	dockerfile := "Dockerfile"
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err != nil {
		// No Dockerfile in the repo — detect a framework and generate one. The rules and
		// templates live in internal/builder, shared with the control plane so the dashboard
		// preview matches exactly what we build here.
		plan, derr := builder.Detect(builder.OSFiles(dir))
		if derr != nil {
			return fmt.Errorf("inspect repository: %w", derr)
		}
		if plan.Status != builder.StatusDetected {
			return errors.New(plan.NextSteps)
		}
		const generated = "Dockerfile.plorigo"
		if err := os.WriteFile(filepath.Join(dir, generated), []byte(plan.Dockerfile), 0o600); err != nil {
			return fmt.Errorf("write generated Dockerfile: %w", err)
		}
		dockerfile = generated
		emit(fmt.Sprintf("no Dockerfile found — generated one for %s (%s, node %s)", plan.RuntimeLabel(), plan.PackageManager, plan.NodeVersion))
		for _, line := range strings.Split(strings.TrimRight(plan.Dockerfile, "\n"), "\n") {
			emit("│ " + line)
		}
	}
	// Build with the same daemon the agent already targets (the CLI honors DOCKER_HOST);
	// DOCKER_BUILDKIT=1 forces BuildKit regardless of the host default. The context is the
	// cloned tree, so no files outside dir are sent.
	cmd := exec.CommandContext(ctx, "docker", "build", "--tag", tag, "--file", dockerfile, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	w := &lineEmitter{emit: emit}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.flush()
	if err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	return nil
}

// detectPort reads the built image's exposed ports (the Dockerfile's `EXPOSE`, plus any
// inherited from base images) and returns the lowest TCP one. It uses `docker image
// inspect` so it sees the fully-resolved image config, not just the app's Dockerfile text.
func (d *dockerClient) detectPort(ctx context.Context, imageTag string) (int32, error) {
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{json .Config.ExposedPorts}}", imageTag).Output()
	if err != nil {
		return 0, fmt.Errorf("inspect built image: %w", err)
	}
	return lowestTCPPort(string(out))
}

// lowestTCPPort parses the JSON ExposedPorts map (e.g. `{"3000/tcp":{},"53/udp":{}}`) and
// returns the lowest TCP port. It errors when the image exposes no TCP port, so the caller
// can ask the user to set one explicitly.
func lowestTCPPort(exposedJSON string) (int32, error) {
	s := strings.TrimSpace(exposedJSON)
	if s == "" || s == "null" {
		return 0, fmt.Errorf("the image exposes no ports")
	}
	var exposed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &exposed); err != nil {
		return 0, fmt.Errorf("parse exposed ports: %w", err)
	}
	ports := make([]int, 0, len(exposed))
	for key := range exposed {
		// Keys look like "3000/tcp"; default to tcp when no protocol is present.
		spec, proto, _ := strings.Cut(key, "/")
		if proto != "" && proto != "tcp" {
			continue
		}
		if n, err := strconv.Atoi(spec); err == nil && n > 0 && n <= 65535 {
			ports = append(ports, n)
		}
	}
	if len(ports) == 0 {
		return 0, fmt.Errorf("the image exposes no TCP port")
	}
	sort.Ints(ports)
	return int32(ports[0]), nil
}

// lineEmitter adapts an io.Writer to a line-oriented emit callback. docker writes stdout
// and stderr from separate goroutines, so Write is mutex-guarded; flush emits any trailing
// partial line after the command exits.
type lineEmitter struct {
	emit func(string)
	mu   sync.Mutex
	buf  []byte
}

func (w *lineEmitter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emitLine(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineEmitter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emitLine(string(w.buf))
		w.buf = nil
	}
}

func (w *lineEmitter) emitLine(line string) {
	if line = strings.TrimRight(line, "\r"); strings.TrimSpace(line) != "" {
		w.emit(line)
	}
}
