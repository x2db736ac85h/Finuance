package httpapi

import (
	"bytes"
	"image"
	"image/jpeg"
	"net/http"
	"testing"
)

// TestAssistantAttachmentsHTTP: a photo is uploaded as image/jpeg, fetched back as a JPEG, refused
// when it isn't one or is too large, refused for a model that doesn't read images, and invisible to
// another user (Phase 20, receipts).
func TestAssistantAttachmentsHTTP(t *testing.T) {
	e, _ := mockEnv(t, nil)
	b := signedIn(t, e, testEmail(t))
	chat := b.do("POST", "/assistant/chats", nil).body["id"].(string)
	var photo bytes.Buffer
	_ = jpeg.Encode(&photo, image.NewGray(image.Rect(0, 0, 32, 24)), nil)

	up := b.raw(http.MethodPost, "/assistant/chats/"+chat+"/attachments", "image/jpeg", bytes.NewReader(photo.Bytes()))
	expect(t, up, 201, "")
	if num(up.body["width"]) != 32 || num(up.body["height"]) != 24 {
		t.Errorf("upload = %s", up.raw)
	}
	id := up.body["id"].(string)
	got := b.do("GET", "/assistant/chats/"+chat+"/attachments/"+id, nil)
	if got.code != 200 || got.header.Get("Content-Type") != "image/jpeg" || !bytes.HasPrefix([]byte(got.raw), []byte{0xff, 0xd8}) {
		t.Errorf("get = %d %s", got.code, got.header)
	}
	expect(t, b.raw(http.MethodPost, "/assistant/chats/"+chat+"/attachments", "image/jpeg", bytes.NewReader([]byte("GIF89a"))), 422, "")
	expect(t, b.raw(http.MethodPost, "/assistant/chats/"+chat+"/attachments", "image/jpeg",
		bytes.NewReader(append([]byte{0xff, 0xd8, 0xff}, make([]byte, 3<<20)...))), 413, "payload_too_large")

	// the mock's models don't read images
	ask := b.do("POST", "/assistant/chats/"+chat+"/messages", map[string]any{"text": "What is this?", "attachmentIds": []string{id},
		"settings": map[string]any{"model": "mock-tools"}})
	expect(t, ask, 422, "")

	other := signedIn(t, e, "other-"+testEmail(t))
	expect(t, other.do("GET", "/assistant/chats/"+chat+"/attachments/"+id, nil), 404, "")
	expect(t, other.raw(http.MethodPost, "/assistant/chats/"+chat+"/attachments", "image/jpeg", bytes.NewReader(photo.Bytes())), 404, "")
}
