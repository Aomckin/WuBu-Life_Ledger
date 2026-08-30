package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"stockroom/internal/ai"
	"stockroom/internal/intake"
)

const (
	maxPreparedImages     = 4
	maxPreparedImageBytes = 6 << 20
	commandTimeout        = 45 * time.Second
)

var ErrProcessing = errors.New("inbox media could not be prepared")

type Prepared struct {
	Images     [][]byte
	Transcript string
}

type Processor struct {
	transcriber ai.Transcriber
}

func New(transcriber ai.Transcriber) *Processor {
	return &Processor{transcriber: transcriber}
}

func (p *Processor) Prepare(ctx context.Context, attachment intake.AttachmentContent) (Prepared, error) {
	mediaType := strings.ToLower(strings.TrimSpace(attachment.MediaType))
	if len(attachment.Data) == 0 {
		return Prepared{}, fmt.Errorf("%w: uploaded file is empty", ErrProcessing)
	}
	switch {
	case mediaType == "image/jpeg" || mediaType == "image/png" || mediaType == "image/webp":
		if len(attachment.Data) <= maxPreparedImageBytes {
			return Prepared{Images: [][]byte{attachment.Data}}, nil
		}
		return p.renderSingleImage(ctx, attachment)
	case mediaType == "image/heic" || mediaType == "image/heif":
		return p.renderSingleImage(ctx, attachment)
	case mediaType == "application/pdf":
		return p.renderPDF(ctx, attachment)
	case strings.HasPrefix(mediaType, "audio/"):
		return p.transcribe(ctx, attachment)
	case strings.HasPrefix(mediaType, "video/"):
		return p.renderVideo(ctx, attachment)
	default:
		return Prepared{}, fmt.Errorf("%w: unsupported media type %q", ErrProcessing, mediaType)
	}
}

func (p *Processor) transcribe(ctx context.Context, attachment intake.AttachmentContent) (Prepared, error) {
	if p.transcriber == nil {
		return Prepared{}, fmt.Errorf("%w: configure AI_TRANSCRIPTION_URL to process audio", ai.ErrTranscriptionUnavailable)
	}
	transcript, err := p.transcriber.Transcribe(ctx, ai.AudioInput{
		FileName:  attachment.FileName,
		MediaType: attachment.MediaType,
		Data:      attachment.Data,
	})
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Transcript: transcript}, nil
}

func (p *Processor) renderSingleImage(ctx context.Context, attachment intake.AttachmentContent) (Prepared, error) {
	directory, inputPath, cleanup, err := p.writeInput(attachment)
	if err != nil {
		return Prepared{}, err
	}
	defer cleanup()
	outputPath := filepath.Join(directory, "image.jpg")
	if err := run(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", inputPath,
		"-frames:v", "1", "-vf", "scale=1600:1600:force_original_aspect_ratio=decrease", "-q:v", "3", outputPath); err != nil {
		return Prepared{}, err
	}
	image, err := readPreparedImage(outputPath)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Images: [][]byte{image}}, nil
}

func (p *Processor) renderPDF(ctx context.Context, attachment intake.AttachmentContent) (Prepared, error) {
	directory, inputPath, cleanup, err := p.writeInput(attachment)
	if err != nil {
		return Prepared{}, err
	}
	defer cleanup()
	prefix := filepath.Join(directory, "page")
	if err := run(ctx, "pdftoppm", "-f", "1", "-l", "3", "-jpeg", "-r", "144", "-scale-to-x", "1600", "-scale-to-y", "-1", inputPath, prefix); err != nil {
		return Prepared{}, err
	}
	images, err := readPreparedImages(prefix + "-*.jpg")
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Images: images}, nil
}

func (p *Processor) renderVideo(ctx context.Context, attachment intake.AttachmentContent) (Prepared, error) {
	directory, inputPath, cleanup, err := p.writeInput(attachment)
	if err != nil {
		return Prepared{}, err
	}
	defer cleanup()
	prefix := filepath.Join(directory, "frame-%02d.jpg")
	if err := run(ctx, "ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", inputPath,
		"-vf", "scale=1280:1280:force_original_aspect_ratio=decrease", "-frames:v", "3", "-q:v", "3", prefix); err != nil {
		return Prepared{}, err
	}
	images, err := readPreparedImages(filepath.Join(directory, "frame-*.jpg"))
	if err != nil {
		return Prepared{}, err
	}
	prepared := Prepared{Images: images}
	if p.transcriber != nil {
		transcript, err := p.transcriber.Transcribe(ctx, ai.AudioInput{
			FileName:  attachment.FileName,
			MediaType: attachment.MediaType,
			Data:      attachment.Data,
		})
		if err == nil {
			prepared.Transcript = transcript
		}
	}
	return prepared, nil
}

func (p *Processor) writeInput(attachment intake.AttachmentContent) (string, string, func(), error) {
	directory, err := os.MkdirTemp("", "stockroom-intake-")
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: create workspace: %v", ErrProcessing, err)
	}
	cleanup := func() { _ = os.RemoveAll(directory) }
	fileName := filepath.Base(strings.ReplaceAll(strings.TrimSpace(attachment.FileName), "\\", "/"))
	if fileName == "." || fileName == "" {
		fileName = "upload.bin"
	}
	inputPath := filepath.Join(directory, fileName)
	if err := os.WriteFile(inputPath, attachment.Data, 0o600); err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("%w: write upload: %v", ErrProcessing, err)
	}
	return directory, inputPath, cleanup, nil
}

func readPreparedImages(pattern string) ([][]byte, error) {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("%w: list extracted images: %v", ErrProcessing, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%w: no visual extract was created", ErrProcessing)
	}
	if len(paths) > maxPreparedImages {
		paths = paths[:maxPreparedImages]
	}
	images := make([][]byte, 0, len(paths))
	for _, path := range paths {
		image, err := readPreparedImage(path)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, nil
}

func readPreparedImage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%w: inspect visual extract: %v", ErrProcessing, err)
	}
	if info.Size() <= 0 || info.Size() > maxPreparedImageBytes {
		return nil, fmt.Errorf("%w: visual extract must be at most %d bytes", ErrProcessing, maxPreparedImageBytes)
	}
	image, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read visual extract: %v", ErrProcessing, err)
	}
	return image, nil
}

func run(ctx context.Context, name string, arguments ...string) error {
	commandContext, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	command := exec.CommandContext(commandContext, name, arguments...)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if len(detail) > 500 {
		detail = detail[:500]
	}
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %s timed out", ErrProcessing, name)
	}
	if detail == "" {
		return fmt.Errorf("%w: %s failed", ErrProcessing, name)
	}
	return fmt.Errorf("%w: %s failed: %s", ErrProcessing, name, detail)
}
