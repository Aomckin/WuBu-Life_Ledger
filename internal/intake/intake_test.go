package intake

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeAndValidate(t *testing.T) {
	input, err := NormalizeAndValidate(Input{
		Source: " Receipt ", Text: " weekly shop ",
		Attachments: []AttachmentInput{{
			ID: "attachment-1", FileName: `C:\uploads\receipt.png`,
			MediaType: "image/png; charset=binary", Data: []byte("image"),
		}},
	})
	if err != nil {
		t.Fatalf("NormalizeAndValidate() error = %v", err)
	}
	if input.Source != SourceReceipt || input.Text != "weekly shop" || input.Attachments[0].FileName != "receipt.png" {
		t.Fatalf("normalized input = %#v", input)
	}
}

func TestNormalizeAndValidateRejectsInvalidInputs(t *testing.T) {
	tests := []Input{
		{Source: SourceCommand},
		{Source: SourceCommand, Text: "used soap", Attachments: []AttachmentInput{{ID: "1", FileName: "a.png", MediaType: "image/png", Data: []byte("x")}}},
		{Source: SourceVoice, Attachments: []AttachmentInput{{ID: "1", FileName: "voice.svg", MediaType: "image/svg+xml", Data: []byte("x")}}},
		{Source: SourcePurchasePhoto, Attachments: []AttachmentInput{
			{ID: "1", FileName: "one.png", MediaType: "image/png", Data: []byte("x")},
			{ID: "2", FileName: "two.png", MediaType: "image/png", Data: []byte("x")},
		}},
		{Source: SourcePurchasePhoto, Attachments: []AttachmentInput{{ID: "1", FileName: "large.mp4", MediaType: "video/mp4", TempPath: "managed", SizeBytes: MaxFileBytes + 1}}},
	}
	for index, input := range tests {
		_, err := NormalizeAndValidate(input)
		var validation *ValidationError
		if !errors.As(err, &validation) || len(validation.Fields) == 0 {
			t.Errorf("case %d error = %v, want ValidationError", index, err)
		}
	}
}

func TestValidStatus(t *testing.T) {
	if !ValidStatus(StatusPending) || !ValidStatus(StatusReviewed) || ValidStatus("processing") {
		t.Fatal("ValidStatus returned unexpected result")
	}
}

func TestNormalizeAndValidateAcceptsOneHundredMegabyteVideo(t *testing.T) {
	input, err := NormalizeAndValidate(Input{
		Source: SourcePurchasePhoto,
		Attachments: []AttachmentInput{{
			ID: "video-1", FileName: "shopping.mp4", MediaType: "video/mp4",
			TempPath: "managed", SizeBytes: MaxFileBytes,
		}},
	})
	if err != nil || input.Attachments[0].SizeBytes != MaxFileBytes {
		t.Fatalf("NormalizeAndValidate() = %#v, %v", input, err)
	}
}

func TestNormalizeAndValidateAcceptsLargeShoppingList(t *testing.T) {
	text := strings.Repeat("shopping item, ", 6_000)
	input, err := NormalizeAndValidate(Input{Source: SourceCommand, Text: text})
	if err != nil || input.Text != strings.TrimSpace(text) {
		t.Fatalf("NormalizeAndValidate() = text length %d, %v", len([]rune(input.Text)), err)
	}
}
