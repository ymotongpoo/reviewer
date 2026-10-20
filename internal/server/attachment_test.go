package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/ymotongpoo/reviewer/internal/store"
)

func TestAttachmentUploadAndRead(t *testing.T) {
	e := newEnv(t)
	id := e.open(t, filepath.Join(e.root, "a"))
	var imageData bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	if err := png.Encode(&imageData, img); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", "screen.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageData.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, e.ts.URL+"/p/"+id+"/api/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range bearer {
		request.Header.Set(k, v)
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	response, err := e.ts.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var attachment store.Attachment
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("upload status = %d", response.StatusCode)
	}
	if err := json.NewDecoder(response.Body).Decode(&attachment); err != nil {
		response.Body.Close()
		t.Fatal(err)
	}
	response.Body.Close()
	if attachment.Name != "screen.png" || attachment.Size != int64(imageData.Len()) {
		t.Fatalf("upload response = %+v", attachment)
	}

	readRequest, err := http.NewRequest(http.MethodGet, e.ts.URL+"/p/"+id+"/api/attachments/"+url.PathEscape(attachment.ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range bearer {
		readRequest.Header.Set(k, v)
	}
	read, err := e.ts.Client().Do(readRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Body.Close()
	got, err := io.ReadAll(read.Body)
	if err != nil {
		t.Fatal(err)
	}
	if read.StatusCode != http.StatusOK || read.Header.Get("Content-Type") != "image/png" || !bytes.Equal(got, imageData.Bytes()) {
		t.Fatalf("read response: status=%d type=%q bytes=%d", read.StatusCode, read.Header.Get("Content-Type"), len(got))
	}
}
