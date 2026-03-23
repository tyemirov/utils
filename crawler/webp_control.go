package crawler

import "errors"

var ErrWebPEncoderTagRequired = errors.New("crawler: webp_encoder build tag is required")

func ValidateWebPEncoderBuild() error {
	return webpEncoderBuildValidationError()
}
