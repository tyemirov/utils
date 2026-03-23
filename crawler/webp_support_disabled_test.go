//go:build !webp_encoder

package crawler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateWebPEncoderBuildDisabled(testingT *testing.T) {
	validateErr := ValidateWebPEncoderBuild()
	require.Error(testingT, validateErr)
	require.ErrorIs(testingT, validateErr, ErrWebPEncoderTagRequired)
}
