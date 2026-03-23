package crawler

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
)

type imageJob struct {
	ProductID   string
	Data        []byte
	Extension   string
	ContentType string
	onSuccess   func()
	onFailure   func()
}

func startImageConverter(jobs chan imageJob, persister FilePersister, logger Logger) func() {
	if jobs == nil || persister == nil {
		return nil
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for job := range jobs {
			processImageJob(job, persister, logger)
		}
	}()
	return func() {
		close(jobs)
		wg.Wait()
	}
}

func processImageJob(job imageJob, persister FilePersister, logger Logger) {
	if len(job.Data) == 0 {
		jobFailed(job)
		return
	}
	if !looksLikeImagePayload(job.ContentType, job.Data) {
		logger.Warning("%s for ProductID %s (content-type=%s)", imageUnavailableMessage, job.ProductID, job.ContentType)
		jobFailed(job)
		return
	}
	webpContent, err := safeConvertToWebP(job.Data, job.Extension)
	if err != nil {
		logger.Warning("%s for ProductID %s: %v", imageUnavailableMessage, job.ProductID, err)
		jobFailed(job)
		return
	}
	fileName := fmt.Sprintf("%s.%s", job.ProductID, webpExtension)
	if err := persister.Save(job.ProductID, fileName, webpContent); err != nil {
		logger.Error("Failed to persist WebP for ProductID %s: %v", job.ProductID, err)
		jobFailed(job)
		return
	}
	jobSucceeded(job)
}

func jobFailed(job imageJob) {
	if job.onFailure != nil {
		job.onFailure()
	}
}

func jobSucceeded(job imageJob) {
	if job.onSuccess != nil {
		job.onSuccess()
	}
}

func looksLikeImagePayload(contentType string, body []byte) bool {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(normalized, "image/") {
		return true
	}
	if len(body) == 0 {
		return false
	}
	sample := body
	if len(sample) > 512 {
		sample = body[:512]
	}
	detected := http.DetectContentType(sample)
	return strings.HasPrefix(strings.ToLower(detected), "image/")
}

func safeConvertToWebP(body []byte, extension string) (result []byte, err error) {
	if convertToWebP == nil {
		return nil, ErrWebPEncoderTagRequired
	}
	prev := debug.SetPanicOnFault(true)
	defer debug.SetPanicOnFault(prev)
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("webp conversion panic: %v", recovered)
		}
	}()
	return convertToWebP(body, extension)
}
