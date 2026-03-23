package crawler

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

type savedFile struct {
	productID string
	fileName  string
	content   []byte
}

type stubFilePersister struct {
	saved   []string
	records []savedFile
	err     error
}

func (stub *stubFilePersister) Save(productID, fileName string, content []byte) error {
	stub.saved = append(stub.saved, productID+":"+fileName)
	stub.records = append(stub.records, savedFile{
		productID: productID,
		fileName:  fileName,
		content:   append([]byte(nil), content...),
	})
	return stub.err
}

func (stub *stubFilePersister) Close() error {
	return nil
}

type capturingLogger struct {
	errors   []string
	warnings []string
}

func (logger *capturingLogger) Debug(string, ...interface{}) {}
func (logger *capturingLogger) Info(string, ...interface{})  {}
func (logger *capturingLogger) Warning(format string, args ...interface{}) {
	logger.warnings = append(logger.warnings, fmt.Sprintf(format, args...))
}
func (logger *capturingLogger) Error(format string, args ...interface{}) {
	logger.errors = append(logger.errors, fmt.Sprintf(format, args...))
}

func testImageBytes(format string) []byte {
	rect := image.Rect(0, 0, 8, 8)
	img := image.NewRGBA(rect)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 20), B: 120, A: 255})
		}
	}
	buffer := new(bytes.Buffer)
	switch format {
	case "png":
		_ = png.Encode(buffer, img)
	case "jpeg":
		_ = jpeg.Encode(buffer, img, &jpeg.Options{Quality: 80})
	default:
		panic(fmt.Sprintf("unsupported format %s", format))
	}
	return buffer.Bytes()
}
