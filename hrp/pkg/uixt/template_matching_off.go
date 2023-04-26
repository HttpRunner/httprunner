//go:build !opencv

package uixt

import (
	"bytes"
	"image"

	"github.com/rs/zerolog/log"
)

func FindAllImageRectsFromRaw(source, search *bytes.Buffer, threshold float32, matchMode ...MatchMode) (rects []image.Rectangle, err error) {
	log.Fatal().Msg("opencv is not supported")
	return
}

func FindImageRectFromRaw(source, search *bytes.Buffer, threshold float32, matchMode ...MatchMode) (rect image.Rectangle, err error) {
	log.Fatal().Msg("opencv is not supported")
	return
}
