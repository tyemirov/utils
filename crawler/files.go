package crawler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NewDirectoryFilePersister creates a file persister that writes to disk.
func NewDirectoryFilePersister(rootDirectory, category, runFolder string) FilePersister {
	return &directoryFilePersister{
		rootDirectory: rootDirectory,
		category:      category,
		runFolder:     runFolder,
	}
}

type directoryFilePersister struct {
	rootDirectory string
	category      string
	runFolder     string
}

func (p *directoryFilePersister) Save(targetID, fileName string, content []byte) error {
	if p == nil || p.rootDirectory == "" || p.category == "" {
		return nil
	}
	runFolder := strings.TrimSpace(p.runFolder)
	if runFolder == "" {
		return fmt.Errorf("crawler: run folder missing")
	}
	dir := filepath.Join(p.rootDirectory, p.category, targetID, runFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fileName), content, 0o644)
}

func (p *directoryFilePersister) Close() error {
	return nil
}
