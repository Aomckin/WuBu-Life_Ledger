package intake

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
	"time"
)

const (
	MaxTextLength              = 100_000
	MaxFileName                = 180
	MaxAttachments             = 1
	MaxFileBytes               = 100 << 20
	MaxRequestBytes            = MaxFileBytes + (1 << 20)
	MaxBatchFiles              = 10
	MaxBatchBytes        int64 = 250 << 20
	MaxBatchRequestBytes       = MaxBatchBytes + (1 << 20)
	AttachmentTTL              = 24 * time.Hour
)

const (
	SourceCommand       = "command"
	SourceReceipt       = "receipt"
	SourcePurchasePhoto = "purchase_photo"
	SourceVoice         = "voice"
	StatusPending       = "pending"
	StatusReviewed      = "reviewed"
	WorkflowQueued      = "queued"
	WorkflowAnalyzing   = "analyzing"
	WorkflowReady       = "ready"
	WorkflowNeedsReview = "needs_review"
	WorkflowFailed      = "failed"
	WorkflowApplied     = "applied"
	WorkflowDiscarded   = "discarded"
)

type AttachmentInput struct {
	ID        string
	FileName  string
	MediaType string
	Data      []byte
	TempPath  string
	SizeBytes int64
}

type Input struct {
	Source      string
	Text        string
	Attachments []AttachmentInput
}

type Attachment struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	ExpiresAt string `json:"expires_at"`
	URL       string `json:"url,omitempty"`
}

type AttachmentContent struct {
	Attachment
	Data []byte `json:"-"`
}

type Entry struct {
	ID            string       `json:"id"`
	BatchID       string       `json:"batch_id,omitempty"`
	Source        string       `json:"source"`
	Text          string       `json:"text"`
	Status        string       `json:"status"`
	WorkflowState string       `json:"workflow_state"`
	FailureReason string       `json:"failure_reason,omitempty"`
	Attachments   []Attachment `json:"attachments"`
	Analysis      *Analysis    `json:"analysis,omitempty"`
	CreatedAt     string       `json:"created_at"`
	UpdatedAt     string       `json:"updated_at"`
}

// Analysis is a persisted, review-only result for one inbox entry. It is
// deliberately separate from inventory mutations: a caller must still review
// and apply any suggested change through the regular inventory API.
type Analysis struct {
	Model       string       `json:"model"`
	Summary     string       `json:"summary"`
	Suggestions []Suggestion `json:"suggestions"`
	NeedsReview bool         `json:"needs_review"`
	ProcessedAt string       `json:"processed_at"`
}

type Suggestion struct {
	Intent     string  `json:"intent"`
	ItemName   string  `json:"item_name"`
	Amount     float64 `json:"amount"`
	Unit       string  `json:"unit"`
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
}

type ListResult struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"`
	Limit   int     `json:"limit"`
	Offset  int     `json:"offset"`
}

type ListFilter struct {
	Status        string
	WorkflowState string
	BatchID       string
	Limit         int
	Offset        int
}

type Settings struct {
	AutoAnalyze bool `json:"auto_analyze"`
}

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "intake validation failed" }

func NormalizeAndValidate(input Input) (Input, error) {
	input.Source = strings.ToLower(strings.TrimSpace(input.Source))
	input.Text = strings.TrimSpace(input.Text)
	fields := map[string]string{}

	if !ValidSource(input.Source) {
		fields["source"] = "Source must be command, receipt, purchase_photo, or voice."
	}
	if len([]rune(input.Text)) > MaxTextLength {
		fields["text"] = fmt.Sprintf("Text must be at most %d characters.", MaxTextLength)
	}
	if len(input.Attachments) > MaxAttachments {
		fields["files"] = fmt.Sprintf("Upload at most %d files.", MaxAttachments)
	}
	if input.Source == SourceCommand && input.Text == "" {
		fields["text"] = "Text is required for a command."
	}
	if input.Source == SourceCommand && len(input.Attachments) > 0 {
		fields["files"] = "Text commands cannot include files."
	}
	if input.Source != "" && input.Source != SourceCommand && len(input.Attachments) == 0 {
		fields["files"] = "At least one file is required."
	}

	for index := range input.Attachments {
		attachment := &input.Attachments[index]
		attachment.FileName = cleanFileName(attachment.FileName)
		attachment.MediaType = normalizeMediaType(attachment.MediaType)
		if attachment.ID == "" {
			fields["files"] = "Each file requires an ID."
			continue
		}
		if attachment.FileName == "" || len([]rune(attachment.FileName)) > MaxFileName {
			fields["files"] = fmt.Sprintf("File names must be between 1 and %d characters.", MaxFileName)
		}
		if attachment.SizeBytes == 0 {
			attachment.SizeBytes = int64(len(attachment.Data))
		}
		if attachment.SizeBytes <= 0 || (attachment.TempPath == "" && len(attachment.Data) == 0) {
			fields["files"] = "Empty files are not supported."
		} else if attachment.SizeBytes > MaxFileBytes {
			fields["files"] = fmt.Sprintf("The file must be at most %d MB.", MaxFileBytes>>20)
		}
		if !mediaTypeAllowed(attachment.MediaType) {
			fields["files"] = "File type is not supported. Upload an image, PDF, audio file, or short video."
		}
	}

	if len(fields) > 0 {
		return Input{}, &ValidationError{Fields: fields}
	}
	return input, nil
}

func ValidSource(value string) bool {
	switch value {
	case SourceCommand, SourceReceipt, SourcePurchasePhoto, SourceVoice:
		return true
	default:
		return false
	}
}

func ValidStatus(value string) bool {
	return value == StatusPending || value == StatusReviewed
}

func ValidWorkflowState(value string) bool {
	switch value {
	case WorkflowQueued, WorkflowAnalyzing, WorkflowReady, WorkflowNeedsReview, WorkflowFailed, WorkflowApplied, WorkflowDiscarded:
		return true
	default:
		return false
	}
}

func normalizeMediaType(value string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return strings.ToLower(mediaType)
}

func mediaTypeAllowed(mediaType string) bool {
	images := map[string]bool{
		"image/jpeg": true, "image/png": true, "image/webp": true,
		"image/heic": true, "image/heif": true,
	}
	audio := map[string]bool{
		"audio/webm": true, "audio/ogg": true, "audio/mp4": true,
		"audio/mpeg": true, "audio/wav": true, "audio/x-wav": true,
	}
	videos := map[string]bool{
		"video/mp4": true, "video/webm": true, "video/quicktime": true,
	}
	return images[mediaType] || audio[mediaType] || videos[mediaType] || mediaType == "application/pdf"
}

func cleanFileName(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	return filepath.Base(value)
}
