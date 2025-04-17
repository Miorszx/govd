package av

import (
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"time"

	"github.com/asticode/go-astiav"
)

func ExtractVideoThumbnail(videoPath string, imagePath string) error {
	formatCtx := astiav.AllocFormatContext()
	defer formatCtx.Free()

	err := formatCtx.OpenInput(videoPath, nil, nil)
	if err != nil {
		return fmt.Errorf("failed opening input: %w", err)
	}

	err = formatCtx.FindStreamInfo(nil)
	if err != nil {
		return fmt.Errorf("failed finding stream info: %w", err)
	}

	stream, codec, err := formatCtx.FindBestStream(
		astiav.MediaTypeVideo, -1, -1)
	if err != nil {
		return fmt.Errorf("failed finding best video stream: %w", err)
	}

	codecParameters := stream.CodecParameters()

	decoder := astiav.FindDecoder(codec.ID())
	if decoder == nil {
		return fmt.Errorf("no decoder found for codec %s", codec.String())
	}

	codecCtx := astiav.AllocCodecContext(decoder)
	defer codecCtx.Free()

	err = codecParameters.ToCodecContext(codecCtx)
	if err != nil {
		return fmt.Errorf("failed setting codec parameters: %w", err)
	}

	err = codecCtx.Open(decoder, nil)
	if err != nil {
		return fmt.Errorf("failed opening codec: %w", err)
	}

	packet := astiav.AllocPacket()
	defer packet.Free()
	frame := astiav.AllocFrame()
	defer frame.Free()

	startTime := time.Now()
	timeout := 5 * time.Second

	// read frames until we find a video frame or timeout
	for time.Since(startTime) < timeout {
		err := formatCtx.ReadFrame(packet)
		if err != nil {
			if errors.Is(err, astiav.ErrEof) {
				return fmt.Errorf("end of file reached before finding video frame")
			}
			return fmt.Errorf("failed reading frame: %w", err)
		}

		if packet.StreamIndex() != stream.Index() {
			packet.Unref()
			continue
		}

		err = codecCtx.SendPacket(packet)
		if err != nil {
			return fmt.Errorf("failed sending packet to decoder: %w", err)
		}

		err = codecCtx.ReceiveFrame(frame)
		if err != nil {
			if errors.Is(err, astiav.ErrEagain) || errors.Is(err, astiav.ErrEof) {
				packet.Unref()
				continue
			}
			return fmt.Errorf("failed receiving frame from decoder: %w", err)
		}

		img, err := frame.Data().GuessImageFormat()
		if err != nil {
			return fmt.Errorf("failed guessing image format: %w", err)
		}
		err = frame.Data().ToImage(img)
		if err != nil {
			return fmt.Errorf("failed converting frame to image: %w", err)
		}
		packet.Unref()

		file, err := os.Create(imagePath)
		if err != nil {
			return fmt.Errorf("failed creating image file: %w", err)
		}
		defer file.Close()

		err = jpeg.Encode(file, img, nil)
		if err != nil {
			return fmt.Errorf("failed encoding image: %w", err)
		}

		return nil
	}

	return fmt.Errorf("timeout while waiting for video frame")
}
