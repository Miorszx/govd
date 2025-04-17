package av

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/asticode/go-astiav"
)

func RemuxFile(inputFile string) error {
	astiav.SetLogLevel(astiav.LogLevelQuiet)

	ext := strings.ToLower(filepath.Ext(inputFile))
	var muxerName string
	switch ext {
	case ".mp4":
		muxerName = "mp4"
	case ".mkv":
		muxerName = "matroska"
	case ".mov":
		muxerName = "mov"
	case ".avi":
		muxerName = "avi"
	default:
		return fmt.Errorf("unsupported output container for extension: %s", ext)
	}
	outputFile := strings.TrimSuffix(inputFile, ext) + ".remuxed" + ext

	inputCtx := astiav.AllocFormatContext()
	if inputCtx == nil {
		return fmt.Errorf("failed to alloc input format context")
	}
	defer inputCtx.Free()

	if err := inputCtx.OpenInput(inputFile, nil, nil); err != nil {
		return fmt.Errorf("failed to open input: %w", err)
	}
	defer inputCtx.CloseInput()

	if err := inputCtx.FindStreamInfo(nil); err != nil {
		return fmt.Errorf("failed to find stream info: %w", err)
	}

	outCtx, err := astiav.AllocOutputFormatContext(nil, muxerName, outputFile)
	if err != nil {
		return fmt.Errorf("failed to alloc output format context: %w", err)
	}
	defer outCtx.Free()

	inToOutIdx := make(map[int]int)
	for inIdx, inStream := range inputCtx.Streams() {
		inCP := inStream.CodecParameters()
		if inCP.CodecID() == astiav.CodecIDNone {
			continue
		}
		if mt := inCP.MediaType(); mt != astiav.MediaTypeVideo && mt != astiav.MediaTypeAudio && mt != astiav.MediaTypeSubtitle {
			continue
		}
		outStream := outCtx.NewStream(nil)
		if outStream == nil {
			return fmt.Errorf("failed to create new stream in output context")
		}
		if err := inCP.Copy(outStream.CodecParameters()); err != nil {
			return fmt.Errorf("failed to copy codec parameters: %w", err)
		}
		outStream.CodecParameters().SetCodecTag(0)
		outStream.SetTimeBase(inStream.TimeBase())
		inToOutIdx[inIdx] = len(inToOutIdx)
	}
	if len(inToOutIdx) == 0 {
		return fmt.Errorf("no supported streams to remux")
	}

	if !outCtx.OutputFormat().Flags().Has(astiav.IOFormatFlagNofile) {
		ioCtx, err := astiav.OpenIOContext(outputFile, astiav.NewIOContextFlags(astiav.IOContextFlagWrite), nil, nil)
		if err != nil {
			return fmt.Errorf("failed to open output IO context: %w", err)
		}
		defer ioCtx.Close()
		outCtx.SetPb(ioCtx)
	}

	if err := outCtx.WriteHeader(nil); err != nil {
		return fmt.Errorf("failed to write output header: %w", err)
	}

	packet := astiav.AllocPacket()
	defer packet.Free()
	for {
		if err := inputCtx.ReadFrame(packet); err != nil {
			if err == astiav.ErrEof {
				break
			}
			return fmt.Errorf("error reading frame: %w", err)
		}
		outIdx, ok := inToOutIdx[packet.StreamIndex()]
		if !ok {
			packet.Unref()
			continue
		}
		inStream := inputCtx.Streams()[packet.StreamIndex()]
		outStream := outCtx.Streams()[outIdx]
		packet.SetPts(astiav.RescaleQRnd(packet.Pts(), inStream.TimeBase(), outStream.TimeBase(), astiav.RoundingNearInf))
		packet.SetDts(astiav.RescaleQRnd(packet.Dts(), inStream.TimeBase(), outStream.TimeBase(), astiav.RoundingNearInf))
		packet.SetDuration(astiav.RescaleQ(packet.Duration(), inStream.TimeBase(), outStream.TimeBase()))
		packet.SetStreamIndex(outIdx)
		packet.SetPos(-1)
		if err := outCtx.WriteInterleavedFrame(packet); err != nil {
			packet.Unref()
			return fmt.Errorf("error writing frame: %w", err)
		}
		packet.Unref()
	}

	if err := outCtx.WriteTrailer(); err != nil {
		return fmt.Errorf("failed to write trailer: %w", err)
	}
	return nil
}
