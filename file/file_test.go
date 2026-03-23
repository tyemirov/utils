package file

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveAll(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	RemoveAll(subDir)
	if _, err := os.Stat(subDir); !os.IsNotExist(err) {
		t.Fatal("expected directory to be removed")
	}
}

func TestRemoveAllNonExistent(t *testing.T) {
	// Should not panic on non-existent path
	RemoveAll("/tmp/nonexistent-path-for-test-1234567890")
}

func TestRemoveAllError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("cannot test permission errors as root")
	}
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.MkdirAll(filepath.Join(child, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove write permission from parent so RemoveAll fails
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })

	// Should not panic, just log
	RemoveAll(child)
}

type errCloser struct{}

func (errCloser) Close() error {
	return errors.New("close error")
}

type okCloser struct{ closed bool }

func (c *okCloser) Close() error {
	c.closed = true
	return nil
}

func TestCloseFile(t *testing.T) {
	c := &okCloser{}
	CloseFile(c)
	if !c.closed {
		t.Fatal("expected closer to be called")
	}
}

func TestCloseFileError(t *testing.T) {
	// Should not panic on close error
	CloseFile(errCloser{})
}

func TestRemoveFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	f.Close()

	RemoveFile(path)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed")
	}
}

func TestRemoveFileNonExistent(t *testing.T) {
	// Should not panic
	RemoveFile("/tmp/nonexistent-file-for-test-1234567890")
}

func TestReadLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lines.txt")
	content := "line1\nline2\nline3"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	lines, err := ReadLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || lines[0] != "line1" || lines[2] != "line3" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestReadLinesFileNotFound(t *testing.T) {
	_, err := ReadLines("/tmp/nonexistent-file-for-test-1234567890")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSaveFile(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "output")
	err := SaveFile(outputDir, "test", []byte("<html></html>"))
	if err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(outputDir, "test.html")
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "<html></html>" {
		t.Fatalf("unexpected content: %s", string(data))
	}
}

func TestSaveFileInvalidDir(t *testing.T) {
	// Try to create inside a file (not a directory)
	tmpFile, err := os.CreateTemp(t.TempDir(), "notadir")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	saveErr := SaveFile(tmpFile.Name(), "test", []byte("data"))
	if saveErr == nil {
		t.Fatal("expected error when output dir is a file")
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "read.txt")
	if err := os.WriteFile(path, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	reader, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 7)
	n, _ := reader.Read(buf)
	if string(buf[:n]) != "content" {
		t.Fatalf("unexpected content: %s", string(buf[:n]))
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, err := ReadFile("/tmp/nonexistent-file-for-test-1234567890")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSaveFileWriteError(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make directory read-only so WriteFile fails
	if err := os.Chmod(outputDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(outputDir, 0o755) })

	err := SaveFile(outputDir, "test", []byte("data"))
	if err == nil {
		t.Fatal("expected write error")
	}
}

func TestReadLinesScannerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.txt")
	// Create a file with a line longer than bufio.MaxScanTokenSize to trigger scanner error
	longLine := make([]byte, 1024*1024) // 1MB line
	for i := range longLine {
		longLine[i] = 'a'
	}
	if err := os.WriteFile(path, longLine, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadLines(path)
	if err == nil {
		t.Fatal("expected scanner error for oversized line")
	}
}
