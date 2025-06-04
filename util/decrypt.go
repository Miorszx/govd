package util

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
)

// decrypts HLS segments using AES-128-CBC
func DecryptSegments(segments []string, key []byte, iv []byte, mediaSequence int) error {
	if !IsValidAESKey(key) {
		return fmt.Errorf("invalid key: expected 16 bytes, got %d", len(key))
	}
	if !IsValidIV(iv) {
		return fmt.Errorf("invalid IV: expected 16 bytes, got %d", len(iv))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}
	for i, segmentPath := range segments {
		if err := decryptSegment(segmentPath, block, iv, mediaSequence+i); err != nil {
			return fmt.Errorf("failed to decrypt segment %s: %w", segmentPath, err)
		}
	}

	return nil
}

// decrypts a single segment file
func decryptSegment(segmentPath string, block cipher.Block, baseIV []byte, segmentSequence int) error {
	encryptedData, err := os.ReadFile(segmentPath)
	if err != nil {
		return fmt.Errorf("failed to read segment file: %w", err)
	}
	if len(encryptedData) == 0 {
		return errors.New("segment file is empty")
	}
	if len(encryptedData)%aes.BlockSize != 0 {
		return errors.New("encrypted data length is not a multiple of block size")
	}
	iv := calculateSegmentIV(baseIV, segmentSequence)
	mode := cipher.NewCBCDecrypter(block, iv)
	decryptedData := make([]byte, len(encryptedData))
	mode.CryptBlocks(decryptedData, encryptedData)
	unpaddedData, err := removePKCS7Padding(decryptedData)
	if err != nil {
		return fmt.Errorf("failed to remove padding: %w", err)
	}
	outputPath := generateDecryptedFilename(segmentPath)
	if err := os.WriteFile(outputPath, unpaddedData, 0644); err != nil {
		return fmt.Errorf("failed to write decrypted file: %w", err)
	}

	return nil
}

// decrypts segments and overwrites the original files
func DecryptSegmentsInPlace(segments []string, key []byte, iv []byte, mediaSequence int) error {
	if !IsValidAESKey(key) {
		return fmt.Errorf("invalid key: expected 16 bytes, got %d", len(key))
	}
	if !IsValidIV(iv) {
		return fmt.Errorf("invalid IV: expected 16 bytes, got %d", len(iv))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}
	for i, segmentPath := range segments {
		if err := decryptSegmentInPlace(segmentPath, block, iv, mediaSequence+i); err != nil {
			return fmt.Errorf("failed to decrypt segment %s: %w", segmentPath, err)
		}
	}
	return nil
}

// decrypts a segment and overwrites the original file
func decryptSegmentInPlace(segmentPath string, block cipher.Block, baseIV []byte, segmentSequence int) error {
	encryptedData, err := os.ReadFile(segmentPath)
	if err != nil {
		return fmt.Errorf("failed to read segment file: %w", err)
	}
	if len(encryptedData) == 0 {
		return errors.New("segment file is empty")
	}
	if len(encryptedData)%aes.BlockSize != 0 {
		return errors.New("encrypted data length is not a multiple of block size")
	}
	iv := calculateSegmentIV(baseIV, segmentSequence)
	mode := cipher.NewCBCDecrypter(block, iv)
	decryptedData := make([]byte, len(encryptedData))
	mode.CryptBlocks(decryptedData, encryptedData)
	unpaddedData, err := removePKCS7Padding(decryptedData)
	if err != nil {
		return fmt.Errorf("failed to remove padding: %w", err)
	}

	// overwrite original file
	if err := os.WriteFile(segmentPath, unpaddedData, 0644); err != nil {
		return fmt.Errorf("failed to write decrypted file: %w", err)
	}

	return nil
}

// decrypts a segment from a reader and writes to a writer
func DecryptSegmentStream(src io.Reader, dst io.Writer, key []byte, iv []byte, mediaSequence int) error {
	if !IsValidAESKey(key) {
		return fmt.Errorf("invalid key: expected 16 bytes, got %d", len(key))
	}
	if !IsValidIV(iv) {
		return fmt.Errorf("invalid IV: expected 16 bytes, got %d", len(iv))
	}
	encryptedData, err := io.ReadAll(src)
	if err != nil {
		return fmt.Errorf("failed to read encrypted data: %w", err)
	}
	if len(encryptedData) == 0 {
		return errors.New("no data to decrypt")
	}
	if len(encryptedData)%aes.BlockSize != 0 {
		return errors.New("encrypted data length is not a multiple of block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}
	segmentIV := calculateSegmentIV(iv, mediaSequence)
	mode := cipher.NewCBCDecrypter(block, segmentIV)
	decryptedData := make([]byte, len(encryptedData))
	mode.CryptBlocks(decryptedData, encryptedData)
	unpaddedData, err := removePKCS7Padding(decryptedData)
	if err != nil {
		return fmt.Errorf("failed to remove padding: %w", err)
	}
	if _, err := dst.Write(unpaddedData); err != nil {
		return fmt.Errorf("failed to write decrypted data: %w", err)
	}
	return nil
}

// decrypts a byte slice representing a single segment
func DecryptSegmentBytes(encryptedData []byte, key []byte, iv []byte, mediaSequence int) ([]byte, error) {
	if !IsValidAESKey(key) {
		return nil, fmt.Errorf("invalid key: expected 16 bytes, got %d", len(key))
	}
	if !IsValidIV(iv) {
		return nil, fmt.Errorf("invalid IV: expected 16 bytes, got %d", len(iv))
	}

	if len(encryptedData) == 0 {
		return nil, errors.New("no data to decrypt")
	}
	if len(encryptedData)%aes.BlockSize != 0 {
		return nil, errors.New("encrypted data length is not a multiple of block size")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	segmentIV := calculateSegmentIV(iv, mediaSequence)
	mode := cipher.NewCBCDecrypter(block, segmentIV)
	decryptedData := make([]byte, len(encryptedData))
	mode.CryptBlocks(decryptedData, encryptedData)
	unpaddedData, err := removePKCS7Padding(decryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to remove padding: %w", err)
	}

	return unpaddedData, nil
}

// calculates the IV for a specific segment using media sequence number
// HLS specification: each segment uses base IV + media sequence number
func calculateSegmentIV(baseIV []byte, mediaSequence int) []byte {
	iv := make([]byte, len(baseIV))
	copy(iv, baseIV)

	// convert media sequence to 32-bit unsigned integer
	seqNum := uint32(mediaSequence)

	// add media sequence to the last 4 bytes of IV (big-endian)
	// this ensures proper overflow handling according to HLS spec
	carry := uint32(0)

	// start from the least significant byte and work backwards
	for i := 15; i >= 12; i-- {
		sum := uint32(iv[i]) + ((seqNum >> (8 * (15 - i))) & 0xFF) + carry
		iv[i] = byte(sum & 0xFF)
		carry = sum >> 8
	}
	// handle any remaining carry into the upper bytes
	for i := 11; i >= 0 && carry > 0; i-- {
		sum := uint32(iv[i]) + carry
		iv[i] = byte(sum & 0xFF)
		carry = sum >> 8
	}

	return iv
}

// removes PKCS#7 padding from decrypted data
func removePKCS7Padding(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}
	paddingLength := int(data[len(data)-1])
	if paddingLength == 0 || paddingLength > aes.BlockSize {
		return nil, fmt.Errorf("invalid padding length: %d", paddingLength)
	}
	if paddingLength > len(data) {
		return nil, fmt.Errorf("padding length (%d) exceeds data length (%d)", paddingLength, len(data))
	}
	for i := len(data) - paddingLength; i < len(data); i++ {
		if data[i] != byte(paddingLength) {
			return nil, fmt.Errorf("invalid padding at position %d", i)
		}
	}
	return data[:len(data)-paddingLength], nil
}

// generates output filename for decrypted segment
func generateDecryptedFilename(originalPath string) string {
	dir := filepath.Dir(originalPath)
	filename := filepath.Base(originalPath)
	ext := filepath.Ext(filename)
	nameWithoutExt := strings.TrimSuffix(filename, ext)

	return filepath.Join(dir, nameWithoutExt+"_decrypted"+ext)
}

func DecryptSegmentsWithSequences(segments []string, key []byte, iv []byte, mediaSequences []int) error {
	if len(segments) != len(mediaSequences) {
		return errors.New("segments and mediaSequences arrays must have the same length")
	}

	if !IsValidAESKey(key) {
		return fmt.Errorf("invalid key: expected 16 bytes, got %d", len(key))
	}
	if !IsValidIV(iv) {
		return fmt.Errorf("invalid IV: expected 16 bytes, got %d", len(iv))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	for i, segmentPath := range segments {
		if err := decryptSegment(segmentPath, block, iv, mediaSequences[i]); err != nil {
			return fmt.Errorf("failed to decrypt segment %s (sequence %d): %w", segmentPath, mediaSequences[i], err)
		}
	}

	return nil
}

// decrypts segments with individual media sequence numbers in place
func DecryptSegmentsWithSequencesInPlace(segments []string, key []byte, iv []byte, mediaSequences []int) error {
	if len(segments) != len(mediaSequences) {
		return errors.New("segments and mediaSequences arrays must have the same length")
	}

	if !IsValidAESKey(key) {
		return fmt.Errorf("invalid key: expected 16 bytes, got %d", len(key))
	}
	if !IsValidIV(iv) {
		return fmt.Errorf("invalid IV: expected 16 bytes, got %d", len(iv))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("failed to create AES cipher: %w", err)
	}

	for i, segmentPath := range segments {
		if err := decryptSegmentInPlace(segmentPath, block, iv, mediaSequences[i]); err != nil {
			return fmt.Errorf("failed to decrypt segment %s (sequence %d): %w", segmentPath, mediaSequences[i], err)
		}
	}

	return nil
}

func IsValidAESKey(key []byte) bool {
	return len(key) == 16
}

func IsValidIV(iv []byte) bool {
	return len(iv) == 16
}

func GenerateZeroIV() []byte {
	return make([]byte, 16)
}
