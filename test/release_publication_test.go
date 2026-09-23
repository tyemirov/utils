package test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasePublicationAllowsOpenPRsButRejectsChangedSource(t *testing.T) {
	helper, err := filepath.Abs("../scripts/release/release_helper.py")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	repository := filepath.Join(root, "repository")
	remote := filepath.Join(root, "remote.git")
	command := func(directory, name string, arguments ...string) string {
		t.Helper()
		cmd := exec.Command(name, arguments...)
		cmd.Dir = directory
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, arguments, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	command(root, "git", "init", "--bare", "--initial-branch=master", remote)
	command(root, "git", "init", "--initial-branch=master", repository)
	for _, setting := range [][2]string{{"user.name", "Release fixture"}, {"user.email", "release@example.test"}, {"commit.gpgsign", "false"}, {"tag.gpgsign", "false"}, {"core.hooksPath", "/dev/null"}} {
		command(repository, "git", "config", setting[0], setting[1])
	}
	command(repository, "git", "remote", "add", "origin", remote)
	command(repository, "git", "commit", "--allow-empty", "-m", "source")
	source := command(repository, "git", "rev-parse", "HEAD")
	command(repository, "git", "push", "origin", "master")
	command(repository, "git", "commit", "--allow-empty", "-m", "release")
	release := command(repository, "git", "rev-parse", "HEAD")
	command(repository, "git", "tag", "-a", "v0.18.0", "-m", "release")
	artifacts := filepath.Join(root, "artifacts")
	command(root, "mkdir", "-p", artifacts)
	write := func(path string, content []byte) {
		t.Helper()
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	notes := []byte("Verified release\n")
	write(filepath.Join(artifacts, "notes.md"), notes)
	manifest, err := json.Marshal(map[string]any{"schema_version": 2, "artifact_kind": "mprlab.release", "version": "v0.18.0", "source_commit": source, "release_commit": release, "default_branch": "master", "notes_sha256": fmt.Sprintf("%x", sha256.Sum256(notes)), "payloads": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(artifacts, "manifest.json"), manifest)
	// The GitHub CLI is the controlled external boundary. Git and the release
	// CLI run unchanged against actual commits, tags, and a local remote.
	write(filepath.Join(root, "github.go"), []byte(`package main
import ("fmt"; "os")
func main() {
 if len(os.Args) > 2 && os.Args[1] == "pr" && os.Args[2] == "list" {
  fmt.Println("[{\"number\":44,\"title\":\"Unrelated change\",\"headRefName\":\"improvement/unrelated\",\"url\":\"https://example.test/44\"}]")
  return
 }
 fmt.Fprintln(os.Stderr, "unexpected GitHub operation", os.Args[1:]); os.Exit(1)
}
`))
	command(root, "go", "build", "-o", filepath.Join(root, "gh"), filepath.Join(root, "github.go"))
	run := func(wantError string) {
		t.Helper()
		cmd := exec.Command("python3", helper, "publish-prepared-release", "--artifact-dir", artifacts, "--dry-run")
		cmd.Dir = repository
		cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
		output, err := cmd.CombinedOutput()
		if wantError == "" {
			if err != nil || !strings.Contains(string(output), `"push_tag": true`) {
				t.Fatalf("prepared release rejected: %v\n%s", err, output)
			}
		} else if err == nil || !strings.Contains(string(output), wantError) {
			t.Fatalf("expected %q: %v\n%s", wantError, err, output)
		}
	}
	run("")
	write(filepath.Join(repository, "uncommitted.txt"), []byte("pending work"))
	run("worktree is dirty")
	if err := os.Remove(filepath.Join(repository, "uncommitted.txt")); err != nil {
		t.Fatal(err)
	}
	// Advance the remote without changing the prepared checkout.
	other := filepath.Join(root, "other")
	command(root, "git", "clone", remote, other)
	command(other, "git", "-c", "user.name=Release fixture", "-c", "user.email=release@example.test", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "commit", "--allow-empty", "-m", "new source")
	command(other, "git", "push", "origin", "master")
	run("remote default branch changed after make release")
}
