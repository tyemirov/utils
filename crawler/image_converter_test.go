package crawler

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessImageJobSkipsNonImages(t *testing.T) {
	logger := &capturingLogger{}
	persister := &stubFilePersister{}
	failCount := 0
	job := imageJob{
		ProductID:   "NONIMG",
		Data:        []byte("<html>captcha</html>"),
		Extension:   ".png",
		ContentType: "text/html; charset=utf-8",
		onFailure: func() {
			failCount++
		},
	}
	processImageJob(job, persister, logger)
	require.Empty(t, persister.records)
	require.NotEmpty(t, logger.warnings)
	require.Equal(t, 1, failCount)
}

func TestProcessImageJobConvertsPNG(t *testing.T) {
	convertToWebP = func(data []byte, _ string) ([]byte, error) { return data, nil }
	defer func() { convertToWebP = nil }()
	logger := &capturingLogger{}
	persister := &stubFilePersister{}
	failCount := 0
	successCount := 0
	job := imageJob{
		ProductID:   "PNG",
		Data:        testImageBytes("png"),
		Extension:   ".png",
		ContentType: "image/png",
		onSuccess: func() {
			successCount++
		},
		onFailure: func() {
			failCount++
		},
	}
	processImageJob(job, persister, logger)
	require.Len(t, persister.records, 1)
	require.Empty(t, logger.errors)
	require.Empty(t, logger.warnings)
	require.Zero(t, failCount)
	require.Equal(t, 1, successCount)
}

func TestProcessImageJobConvertsJPEG(t *testing.T) {
	convertToWebP = func(data []byte, _ string) ([]byte, error) { return data, nil }
	defer func() { convertToWebP = nil }()
	logger := &capturingLogger{}
	persister := &stubFilePersister{}
	failCount := 0
	job := imageJob{
		ProductID:   "JPG",
		Data:        testImageBytes("jpeg"),
		Extension:   ".jpg",
		ContentType: "image/jpeg",
		onFailure: func() {
			failCount++
		},
	}
	processImageJob(job, persister, logger)
	require.Len(t, persister.records, 1)
	require.Empty(t, logger.errors)
	require.Zero(t, failCount)
}

func TestProcessImageJobHandlesConversionPanic(t *testing.T) {
	logger := &capturingLogger{}
	persister := &stubFilePersister{}
	original := convertToWebP
	convertToWebP = func([]byte, string) ([]byte, error) { panic("webp crash") }
	defer func() { convertToWebP = original }()

	failCount := 0
	job := imageJob{
		ProductID:   "FAIL",
		Data:        testImageBytes("png"),
		Extension:   ".png",
		ContentType: "image/png",
		onFailure: func() {
			failCount++
		},
	}
	processImageJob(job, persister, logger)
	require.Empty(t, persister.records)
	require.NotEmpty(t, logger.warnings)
	require.Empty(t, logger.errors)
	require.Equal(t, 1, failCount)
}

func TestProcessImageJobFailsOnConversionError(t *testing.T) {
	logger := &capturingLogger{}
	persister := &stubFilePersister{}
	original := convertToWebP
	convertToWebP = func([]byte, string) ([]byte, error) {
		return nil, errors.New("encode failure")
	}
	defer func() { convertToWebP = original }()

	failCount := 0
	job := imageJob{
		ProductID:   "ERR",
		Data:        testImageBytes("png"),
		Extension:   ".PNG",
		ContentType: "image/png",
		onFailure: func() {
			failCount++
		},
	}
	processImageJob(job, persister, logger)
	require.Empty(t, persister.records)
	require.NotEmpty(t, logger.warnings)
	require.Empty(t, logger.errors)
	require.Equal(t, 1, failCount)
}

func TestProcessImageJobFailsWhenPersisterErrors(t *testing.T) {
	convertToWebP = func(data []byte, _ string) ([]byte, error) { return data, nil }
	defer func() { convertToWebP = nil }()
	logger := &capturingLogger{}
	persister := &stubFilePersister{err: errors.New("disk failure")}

	job := imageJob{
		ProductID:   "ERR",
		Data:        testImageBytes("png"),
		Extension:   ".png",
		ContentType: "image/png",
		onFailure: func() {
			logger.Error("fallback triggered")
		},
	}

	processImageJob(job, persister, logger)

	require.NotEmpty(t, logger.errors)
	require.Empty(t, logger.warnings)
}
