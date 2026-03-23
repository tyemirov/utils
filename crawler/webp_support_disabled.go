//go:build !webp_encoder

package crawler

import "fmt"

func webpEncoderBuildValidationError() error {
	return fmt.Errorf("%w; rebuild with -tags webp_encoder", ErrWebPEncoderTagRequired)
}
