// Package convert wraps vips to produce pyramidal TIFFs.
package convert

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const errMsgVipsTiffsave = "vips tiffsave"

// Converter converts an original scan to a pyramidal TIFF.
type Converter interface {
	Convert(ctx context.Context, srcPath, dstPath string) error
}

// VipsConverter invokes vips on the host to build a pyramidal TIFF.
// vips must be installed and on PATH.
// TileWidth and TileHeight are the pixel dimensions of each tile; both default to 256.
type VipsConverter struct {
	TileWidth  int
	TileHeight int
}

// Convert runs:
//
//	vips tiffsave <srcPath> <dstPath> --tile --pyramid --compression jpeg \
//	    --tile-width <TileWidth> --tile-height <TileHeight>
func (converter VipsConverter) Convert(ctx context.Context, srcPath, dstPath string) error {
	tileWidth := converter.TileWidth
	if tileWidth == 0 {
		tileWidth = 256
	}
	tileHeight := converter.TileHeight
	if tileHeight == 0 {
		tileHeight = 256
	}

	vipsOutput, err := exec.CommandContext(ctx,
		"vips", "tiffsave", srcPath, dstPath,
		"--tile", "--pyramid",
		"--compression", "jpeg",
		"--tile-width", strconv.Itoa(tileWidth),
		"--tile-height", strconv.Itoa(tileHeight),
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", errMsgVipsTiffsave, err, strings.TrimSpace(string(vipsOutput)))
	}
	return nil
}
