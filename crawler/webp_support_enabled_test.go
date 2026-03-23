//go:build webp_encoder

package crawler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateWebPEncoderBuildEnabled(testingT *testing.T) {
	require.NoError(testingT, ValidateWebPEncoderBuild())
}
