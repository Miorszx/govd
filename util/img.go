package util

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"slices"

	_ "image/gif"
	_ "image/png"

	_ "github.com/strukturag/libheif/go/heif"
	_ "golang.org/x/image/webp"
)

var (
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	pngMagic  = []byte{0x89, 0x50, 0x4E, 0x47}
	gifMagic  = []byte{0x47, 0x49, 0x46}
	riffMagic = []byte{0x52, 0x49, 0x46, 0x46}
	webpMagic = []byte{0x57, 0x45, 0x42, 0x50}
)

func ImgToJPEG(file io.ReadSeeker, outputPath string) error {
	format, err := DetectImageFormat(file)
	if err != nil {
		return fmt.Errorf("failed to detect image format: %w", err)
	}

	outputFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer outputFile.Close()

	if format == "jpeg" {
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			os.Remove(outputPath)
			return fmt.Errorf("failed to reset file position: %w", err)
		}

		if _, err = io.Copy(outputFile, file); err != nil {
			os.Remove(outputPath)
			return fmt.Errorf("failed to copy image: %w", err)
		}
		return nil
	}

	if _, err = file.Seek(0, io.SeekStart); err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to reset file position: %w", err)
	}

	img, _, err := image.Decode(file)
	if err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to decode image: %w", err)
	}

	err = jpeg.Encode(outputFile, img, nil)
	if err != nil {
		os.Remove(outputPath)
		return fmt.Errorf("failed to encode image: %w", err)
	}

	return nil
}

func DetectImageFormat(file io.ReadSeeker) (string, error) {
	header := make([]byte, 12)

	_, err := file.Read(header)
	if err != nil {
		return "", fmt.Errorf("failed to read file header: %w", err)
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("failed to reset file position: %w", err)
	}
	if len(header) < 12 {
		return "", ErrFileTooShort
	}
	if bytes.HasPrefix(header, jpegMagic) {
		return "jpeg", nil
	}

	if bytes.HasPrefix(header, pngMagic) {
		return "png", nil
	}

	if bytes.HasPrefix(header, gifMagic) {
		return "gif", nil
	}

	if isHEIF(header) {
		return "heif", nil
	}

	if bytes.HasPrefix(header, riffMagic) {
		if bytes.Equal(header[8:12], webpMagic) {
			return "webp", nil
		}
		return "", ErrUnknownRIFF
	}

	return "", ErrUnsupportedImageFormat
}

func isHEIF(header []byte) bool {
	isHeifHeader := header[0] == 0x00 && header[1] == 0x00 &&
		header[2] == 0x00 && (header[3] == 0x18 || header[3] == 0x1C) &&
		bytes.Equal(header[4:8], []byte("ftyp"))
	if !isHeifHeader {
		return false
	}
	heifBrands := []string{"heic", "heix", "mif1", "msf1"}
	brand := string(header[8:12])

	return slices.Contains(heifBrands, brand)
}
