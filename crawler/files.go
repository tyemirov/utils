package crawler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type directoryFilePersister struct {
	rootDirectory string
	platformID    string
	runFolder     string
}

func newDirectoryFilePersister(rootDirectory, platformID, runFolder string) FilePersister {
	return &directoryFilePersister{
		rootDirectory: rootDirectory,
		platformID:    platformID,
		runFolder:     runFolder,
	}
}

func (persister *directoryFilePersister) Save(productID, fileName string, content []byte) error {
	if persister == nil {
		return nil
	}
	if persister.rootDirectory == "" || persister.platformID == "" {
		return nil
	}
	runFolder := strings.TrimSpace(persister.runFolder)
	if runFolder == "" {
		return fmt.Errorf("crawler: run folder missing")
	}
	productDirectory := filepath.Join(persister.rootDirectory, persister.platformID, productID, runFolder)
	if err := os.MkdirAll(productDirectory, 0o755); err != nil {
		return err
	}
	outputPath := filepath.Join(productDirectory, fileName)
	return os.WriteFile(outputPath, content, 0o644)
}

func (persister *directoryFilePersister) Close() error {
	return nil
}
