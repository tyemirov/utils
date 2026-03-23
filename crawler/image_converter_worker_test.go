package crawler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartImageConverterNoopWhenDisabled(t *testing.T) {
	logger := &capturingLogger{}
	persister := &stubFilePersister{}

	require.Nil(t, startImageConverter(nil, persister, logger))
	require.Nil(t, startImageConverter(make(chan imageJob), nil, logger))
}

func TestStartImageConverterProcessesJobsAndStops(t *testing.T) {
	convertToWebP = func(data []byte, _ string) ([]byte, error) { return data, nil }
	defer func() { convertToWebP = nil }()
	logger := &capturingLogger{}
	persister := &stubFilePersister{}
	jobs := make(chan imageJob, 2)

	stop := startImageConverter(jobs, persister, logger)
	require.NotNil(t, stop)

	jobs <- imageJob{
		ProductID:   "IMG",
		Data:        testImageBytes("png"),
		Extension:   ".png",
		ContentType: "image/png",
	}

	failCount := 0
	jobs <- imageJob{
		ProductID:   "BAD",
		Data:        []byte("html"),
		Extension:   ".png",
		ContentType: "text/html",
		onFailure: func() {
			failCount++
		},
	}

	stop()

	require.Len(t, persister.records, 1)
	require.Equal(t, 1, failCount)
}
