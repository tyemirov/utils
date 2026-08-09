package test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseLifecycleUsesRepositoryOwnedTooling(t *testing.T) {
	repositoryRoot := filepath.Clean("..")
	makefilePath := filepath.Join(repositoryRoot, "Makefile")
	makefile, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatalf("read %s: %v", makefilePath, err)
	}

	makefileText := string(makefile)
	for _, contract := range []string{
		"RELEASE_HELPER := $(abspath $(CURDIR)/scripts/release/release_helper.py)",
		"RELEASE_TOOL_DIR := $(abspath $(CURDIR)/scripts/release)",
	} {
		if !strings.Contains(makefileText, contract) {
			t.Fatalf("Makefile does not declare %q", contract)
		}
	}
	if strings.Contains(makefileText, "agentSkills/gitrelease") {
		t.Fatal("Makefile still references sibling release tooling")
	}

	for _, script := range []string{
		"prepare_release.sh",
		"publish_release.sh",
		"release_helper.py",
		"prepare_go_module_artifact.sh",
		"deploy_go_module_artifact.sh",
		"go_module_artifact.py",
	} {
		path := filepath.Join(repositoryRoot, "scripts", "release", script)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode()&0o111 == 0 {
			t.Fatalf("%s is not executable", path)
		}
	}

	command := exec.Command("make", "--dry-run", "release", "go-module-artifact", "publish", "deploy")
	command.Dir = repositoryRoot
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("dry-run release lifecycle: %v\n%s", err, output)
	}
	outputText := string(output)
	for _, script := range []string{
		"scripts/release/prepare_release.sh",
		"scripts/release/prepare_go_module_artifact.sh",
		"scripts/release/publish_release.sh",
		"scripts/release/deploy_go_module_artifact.sh",
	} {
		if !strings.Contains(outputText, script) {
			t.Fatalf("dry-run output does not use %s:\n%s", script, outputText)
		}
	}
	if strings.Contains(outputText, "agentSkills/gitrelease") {
		t.Fatalf("dry-run output references sibling release tooling:\n%s", outputText)
	}
}
