package builder

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mapFiles is an in-memory Files: present keys map to their contents, missing keys are absent.
type mapFiles map[string]string

func (m mapFiles) ReadFile(p string) ([]byte, bool, error) {
	v, ok := m[p]
	if !ok {
		return nil, false, nil
	}
	return []byte(v), true, nil
}

// errFiles fails on a chosen path, to prove Detect surfaces a real read error.
type errFiles struct{ failOn string }

func (e errFiles) ReadFile(p string) ([]byte, bool, error) {
	if p == e.failOn {
		return nil, false, errors.New("boom")
	}
	return nil, false, nil
}

func TestDetect_NonFramework(t *testing.T) {
	cases := []struct {
		name      string
		files     mapFiles
		want      Status
		nextSteps string
	}{
		{"dockerfile wins", mapFiles{"Dockerfile": "FROM scratch", "package.json": `{"dependencies":{"next":"14"}}`}, StatusHasDockerfile, ""},
		{"no package.json", mapFiles{"README.md": "hi"}, StatusUnsupported, stepsNoPackageJSON},
		{"bad package.json", mapFiles{"package.json": "{not json"}, StatusUnsupported, stepsBadPackageJSON},
		{"node without start", mapFiles{"package.json": `{"dependencies":{"express":"4"}}`}, StatusUnsupported, stepsNodeNoStart},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Detect(c.files)
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if got.Status != c.want {
				t.Fatalf("status = %q, want %q", got.Status, c.want)
			}
			if c.nextSteps != "" && got.NextSteps != c.nextSteps {
				t.Fatalf("next steps = %q, want %q", got.NextSteps, c.nextSteps)
			}
			if c.want != StatusDetected && got.Dockerfile != "" {
				t.Fatalf("non-detected plan should have no Dockerfile, got %q", got.Dockerfile)
			}
		})
	}
}

func TestDetect_Next(t *testing.T) {
	got, err := Detect(mapFiles{
		"package.json":   `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14.2.0"}}`,
		"pnpm-lock.yaml": "lockfileVersion: '9.0'",
		".nvmrc":         "20",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	assertEq(t, "status", string(got.Status), string(StatusDetected))
	assertEq(t, "runtime", got.Runtime, RuntimeNext)
	assertEq(t, "label", got.RuntimeLabel(), "Next.js")
	assertEq(t, "pm", got.PackageManager, "pnpm")
	assertEq(t, "node", got.NodeVersion, "20")
	assertEq(t, "build", got.BuildCommand, "pnpm run build")
	assertEq(t, "start", got.StartCommand, "pnpm run start")
	if got.Port != 3000 {
		t.Errorf("port = %d, want 3000", got.Port)
	}
	for _, want := range []string{
		"FROM node:20-bookworm-slim",
		"RUN corepack enable",
		"RUN pnpm install",
		"RUN pnpm run build",
		"ENV PORT=3000",
		"EXPOSE 3000",
		`CMD ["sh", "-c", "pnpm run start"]`,
	} {
		if !strings.Contains(got.Dockerfile, want) {
			t.Errorf("Dockerfile missing %q\n---\n%s", want, got.Dockerfile)
		}
	}
}

func TestDetect_NextNpmNoLock(t *testing.T) {
	got, err := Detect(mapFiles{
		"package.json": `{"scripts":{"build":"next build","start":"next start"},"dependencies":{"next":"14"}}`,
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	assertEq(t, "pm", got.PackageManager, "npm")
	assertEq(t, "node", got.NodeVersion, defaultNodeMajor)
	// No lockfile -> `npm install`, not `npm ci`; npm needs no corepack.
	if strings.Contains(got.Dockerfile, "npm ci") {
		t.Errorf("expected `npm install` without a lockfile, got:\n%s", got.Dockerfile)
	}
	mustContain(t, got.Dockerfile, "RUN npm install")
	if strings.Contains(got.Dockerfile, "corepack enable") {
		t.Errorf("npm should not enable corepack:\n%s", got.Dockerfile)
	}
}

func TestDetect_Vite(t *testing.T) {
	got, err := Detect(mapFiles{
		"package.json": `{"scripts":{"build":"vite build"},"devDependencies":{"vite":"5.0.0"}}`,
		"yarn.lock":    "# yarn",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	assertEq(t, "runtime", got.Runtime, RuntimeVite)
	assertEq(t, "pm", got.PackageManager, "yarn")
	assertEq(t, "build", got.BuildCommand, "yarn build")
	for _, want := range []string{
		"AS build",
		"RUN corepack enable",
		"RUN yarn install",
		"RUN yarn build",
		"RUN npm install -g serve@14",
		"COPY --from=build /app/dist ./dist",
		`CMD ["sh", "-c", "serve -s dist -l 3000"]`,
	} {
		mustContain(t, got.Dockerfile, want)
	}
}

func TestDetect_Node(t *testing.T) {
	got, err := Detect(mapFiles{
		"package.json":      `{"scripts":{"start":"node server.js"},"engines":{"node":">=18"}}`,
		"package-lock.json": "{}",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	assertEq(t, "runtime", got.Runtime, RuntimeNode)
	assertEq(t, "node", got.NodeVersion, "18") // from engines, no .nvmrc
	assertEq(t, "build", got.BuildCommand, "") // no build script
	assertEq(t, "start", got.StartCommand, "npm run start")
	mustContain(t, got.Dockerfile, "RUN npm ci || npm install") // lockfile present, with a fallback
	if strings.Contains(got.Dockerfile, "RUN npm run build") {
		t.Errorf("node without a build script should have no build step:\n%s", got.Dockerfile)
	}
}

func TestDetect_BunBaseImage(t *testing.T) {
	got, err := Detect(mapFiles{
		"package.json": `{"scripts":{"start":"bun run index.ts"},"dependencies":{}}`,
		"bun.lockb":    "binary",
	})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	assertEq(t, "pm", got.PackageManager, "bun")
	mustContain(t, got.Dockerfile, "FROM oven/bun:1")
	mustContain(t, got.Dockerfile, "RUN bun install")
}

func TestDetect_PropagatesReadError(t *testing.T) {
	if _, err := Detect(errFiles{failOn: "Dockerfile"}); err == nil {
		t.Fatal("expected a read error to propagate")
	}
	if _, err := Detect(errFiles{failOn: "package.json"}); err == nil {
		t.Fatal("expected a read error to propagate")
	}
}

func TestOSFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f := OSFiles(dir)
	if _, ok, err := f.ReadFile("package.json"); err != nil || !ok {
		t.Fatalf("present file: ok=%v err=%v", ok, err)
	}
	if _, ok, err := f.ReadFile("nope.txt"); err != nil || ok {
		t.Fatalf("absent file: ok=%v err=%v", ok, err)
	}
	// A traversal attempt is cleaned to repo-relative, so it can't read outside the root.
	if _, ok, _ := f.ReadFile("../../../../etc/hosts"); ok {
		t.Fatal("path traversal should not resolve outside the root")
	}
}

func assertEq(t *testing.T, what, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q, want %q", what, got, want)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("missing %q in:\n%s", needle, haystack)
	}
}
