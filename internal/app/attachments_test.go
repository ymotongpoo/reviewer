package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/store"
)

func TestCommentImageAttachmentFeedback(t *testing.T) {
	a, _ := setup(t)

	var imageData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}

	attachment, err := a.UploadAttachment("../../screen shot.png", imageData.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if attachment.Name != "screen shot.png" || attachment.Size != int64(imageData.Len()) {
		t.Fatalf("attachment = %+v", attachment)
	}
	got, mediaType, err := a.Attachment(attachment.ID)
	if err != nil || mediaType != "image/png" || !bytes.Equal(got, imageData.Bytes()) {
		t.Fatalf("attachment read: mediaType=%q err=%v", mediaType, err)
	}
	if _, _, err := a.Attachment("../../state.json"); err == nil {
		t.Fatal("invalid attachment id was accepted")
	}
	if _, err := a.UploadAttachment("not-image.png", []byte("not an image")); err == nil {
		t.Fatal("invalid image was accepted")
	}

	comment, err := a.CreateComment(NewComment{
		Scope: store.ScopeProject, Body: "この画像を確認してください",
		Attachments: []store.Attachment{attachment},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(comment.Attachments) != 1 || comment.Attachments[0].ID != attachment.ID {
		t.Fatalf("saved comment attachments = %+v", comment.Attachments)
	}

	result, err := a.Submit()
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := os.ReadFile(result.FeedbackPath)
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.ToSlash(filepath.Join("..", "..", "attachments", attachment.ID))
	if !strings.Contains(string(markdown), "![screen shot.png](<"+wantPath+">)") {
		t.Fatalf("feedback.md does not link the attachment at %q:\n%s", wantPath, markdown)
	}
	feedbackJSON, err := os.ReadFile(result.FeedbackJSON)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Comments []struct {
			Attachments []struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"attachments"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(feedbackJSON, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Comments) != 1 || len(doc.Comments[0].Attachments) != 1 ||
		doc.Comments[0].Attachments[0].Name != "screen shot.png" || doc.Comments[0].Attachments[0].Path != wantPath {
		t.Fatalf("feedback.json attachment = %+v", doc.Comments)
	}
}

func TestCommentAttachmentsCanBeCleared(t *testing.T) {
	a, _ := setup(t)
	attachment, err := a.UploadAttachment("image.png", validTestPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.CreateComment(NewComment{Scope: store.ScopeProject, Attachments: []store.Attachment{attachment}})
	if err != nil {
		t.Fatal(err)
	}
	empty := []store.Attachment{}
	updated, err := a.UpdateComment(c.ID, CommentPatch{Attachments: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Attachments) != 0 {
		t.Fatalf("attachments were not cleared: %+v", updated.Attachments)
	}
}

func validTestPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
