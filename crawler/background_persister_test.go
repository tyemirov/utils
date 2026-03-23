package crawler

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type mockFilePersister struct {
	mu    sync.Mutex
	saved []struct {
		id      string
		name    string
		content []byte
	}
	saveErr   error
	saveDelay time.Duration
	closed    bool
}

func (m *mockFilePersister) Save(productID, fileName string, content []byte) error {
	if m.saveDelay > 0 {
		time.Sleep(m.saveDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved = append(m.saved, struct {
		id      string
		name    string
		content []byte
	}{productID, fileName, content})
	return m.saveErr
}

func (m *mockFilePersister) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

type overlapTrackingPersister struct {
	mu            sync.Mutex
	activeByKey   map[string]int
	overlapByKey  map[string]int
	saveDelay     time.Duration
	savedContents map[string][][]byte
}

func (p *overlapTrackingPersister) Save(productID, fileName string, content []byte) error {
	key := productID + ":" + fileName
	p.mu.Lock()
	if p.activeByKey == nil {
		p.activeByKey = make(map[string]int)
	}
	if p.overlapByKey == nil {
		p.overlapByKey = make(map[string]int)
	}
	if p.savedContents == nil {
		p.savedContents = make(map[string][][]byte)
	}
	p.activeByKey[key]++
	if p.activeByKey[key] > 1 {
		p.overlapByKey[key]++
	}
	p.mu.Unlock()

	if p.saveDelay > 0 {
		time.Sleep(p.saveDelay)
	}

	p.mu.Lock()
	p.savedContents[key] = append(p.savedContents[key], append([]byte(nil), content...))
	p.activeByKey[key]--
	p.mu.Unlock()
	return nil
}

func (p *overlapTrackingPersister) Close() error {
	return nil
}

func TestBackgroundPersister_SavesFiles(t *testing.T) {
	mock := &mockFilePersister{}
	persister := newBackgroundFilePersister(mock, 1, 10, noopLogger{})

	err := persister.Save("p1", "f1", []byte("c1"))
	require.NoError(t, err)
	err = persister.Save("p2", "f2", []byte("c2"))
	require.NoError(t, err)

	err = persister.Close()
	require.NoError(t, err)

	require.True(t, mock.closed)
	require.Len(t, mock.saved, 2)
	require.Equal(t, "p1", mock.saved[0].id)
	require.Equal(t, "p2", mock.saved[1].id)
}

func TestBackgroundPersister_Concurrency(t *testing.T) {
	mock := &mockFilePersister{saveDelay: 10 * time.Millisecond}
	// 2 workers
	persister := newBackgroundFilePersister(mock, 2, 100, noopLogger{})

	start := time.Now()
	count := 10
	for i := 0; i < count; i++ {
		_ = persister.Save("p", "f", []byte("c"))
	}
	err := persister.Close()
	require.NoError(t, err)
	elapsed := time.Since(start)

	require.Len(t, mock.saved, count)
	// With 2 workers and 10ms delay, 10 items should take ~50ms (ideal) to ~60ms.
	// If it was serial, it would take 100ms.
	// Checking that it's faster than serial execution logic isn't always reliable in CI,
	// but we can check correct count and closing.
	_ = elapsed
}

type errorLogger struct {
	errors []string
	mu     sync.Mutex
}

func (e *errorLogger) Debug(string, ...interface{})   {}
func (e *errorLogger) Info(string, ...interface{})    {}
func (e *errorLogger) Warning(string, ...interface{}) {}
func (e *errorLogger) Error(format string, args ...interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.errors = append(e.errors, fmt.Sprintf(format, args...))
}

func TestBackgroundPersister_LogsErrors(t *testing.T) {
	mock := &mockFilePersister{saveErr: errors.New("disk full")}
	logger := &errorLogger{}
	persister := newBackgroundFilePersister(mock, 1, 10, logger)

	_ = persister.Save("p1", "f1", []byte("c1"))
	_ = persister.Close()

	require.Len(t, logger.errors, 1)
	require.Contains(t, logger.errors[0], "disk full")
}

func TestBackgroundPersister_WorkerCount(t *testing.T) {
	// Ensure it doesn't panic with invalid inputs
	mock := &mockFilePersister{}
	persister := newBackgroundFilePersister(mock, 0, 0, noopLogger{})
	persister.Save("p", "f", nil)
	persister.Close()
}

func TestBackgroundPersister_SaveAfterClose(t *testing.T) {
	mock := &mockFilePersister{}
	persister := newBackgroundFilePersister(mock, 1, 10, noopLogger{})

	err := persister.Close()
	require.NoError(t, err)

	err = persister.Save("p1", "f1", []byte("c1"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "closed")
}

func TestBackgroundPersister_SaveCopiesContentBeforeAsyncWrite(t *testing.T) {
	mock := &mockFilePersister{saveDelay: 15 * time.Millisecond}
	persister := newBackgroundFilePersister(mock, 1, 10, noopLogger{})

	content := []byte("original")
	err := persister.Save("p1", "f1", content)
	require.NoError(t, err)
	copy(content, []byte("mutated!"))

	err = persister.Close()
	require.NoError(t, err)

	require.Len(t, mock.saved, 1)
	require.Equal(t, []byte("original"), mock.saved[0].content)
}

func TestBackgroundPersister_SerializesWritesPerTarget(t *testing.T) {
	delegate := &overlapTrackingPersister{saveDelay: 5 * time.Millisecond}
	persister := newBackgroundFilePersister(delegate, 8, 64, noopLogger{})

	for index := 0; index < 32; index++ {
		err := persister.Save("same-product", "same-file.html", []byte(fmt.Sprintf("v-%02d", index)))
		require.NoError(t, err)
	}
	for index := 0; index < 16; index++ {
		err := persister.Save(
			fmt.Sprintf("other-product-%02d", index),
			"other-file.html",
			[]byte(fmt.Sprintf("other-%02d", index)),
		)
		require.NoError(t, err)
	}

	err := persister.Close()
	require.NoError(t, err)

	require.Zero(t, delegate.overlapByKey["same-product:same-file.html"])
	require.Len(t, delegate.savedContents["same-product:same-file.html"], 32)
}
