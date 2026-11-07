package assistant

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

func testPhoto(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for x := range 40 {
		img.Set(x, x%30, color.Black)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestAttachments: a photo waits in its chat, goes to a model that reads images with its question
// only, shows on that question, and never reaches another user (Phase 20, receipts).
func TestAttachments(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	photo := testPhoto(t)

	a, err := re.chats.AddAttachment(re.ctx, re.user, chat, photo)
	if err != nil || a.Width != 40 || a.Height != 30 {
		t.Fatalf("add: %v %+v", err, a)
	}
	var ve *platform.ValidationError
	if _, err := re.chats.AddAttachment(re.ctx, re.user, chat, []byte("not a photo")); !errors.As(err, &ve) {
		t.Errorf("not a photo: %v", err)
	}
	if _, err := re.chats.AddAttachment(re.ctx, re.user, uuid.New(), photo); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("unknown chat: %v", err)
	}

	// a model that doesn't read images refuses, before anything is sent or stored
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "Read it", Connection: "mock", Model: "mock-tools",
		AttachmentIDs: []uuid.UUID{a.ID}}); !errors.As(err, &ve) || !strings.Contains(err.Error(), "doesn't read images") {
		t.Errorf("no vision: %v", err)
	}
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "Read it", Connection: "mock", Model: "mock-vl",
		AttachmentIDs: []uuid.UUID{uuid.New()}}); !errors.As(err, &ve) {
		t.Errorf("unknown photo: %v", err)
	}

	re.mock.Push(llmmock.Reply{Content: "A receipt from a bakery."}, llmmock.Reply{Content: "Nothing else."})
	re.ask(chat, "What is on this receipt?", func(r *Request) { r.Model, r.AttachmentIDs = "mock-vl", []uuid.UUID{a.ID, a.ID} })
	re.ask(chat, "And the date?", func(r *Request) { r.Model = "mock-vl" })
	reqs := re.mock.Requests()
	images := func(i int) int {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(reqs[i], &body)
		n := 0
		for _, m := range body.Messages {
			n += strings.Count(string(m.Content), "data:image/jpeg;base64,")
		}
		return n
	}
	if len(reqs) < 2 || images(0) != 1 || images(len(reqs)-1) != 0 {
		t.Errorf("images per request: first %d, last %d (the photo goes with its question only)", images(0), images(len(reqs)-1))
	}

	_, msgs, err := re.chats.Get(re.ctx, re.user, chat)
	if err != nil {
		t.Fatal(err)
	}
	var shown []Attachment
	for _, m := range msgs {
		shown = append(shown, m.Attachments...)
	}
	if len(shown) != 1 || shown[0].ID != a.ID || msgs[0].Attachments == nil {
		t.Errorf("attachments shown = %+v", shown)
	}
	// sent once: it can't go with another question
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "Again", Connection: "mock", Model: "mock-vl",
		AttachmentIDs: []uuid.UUID{a.ID}}); !errors.As(err, &ve) {
		t.Errorf("a sent photo again: %v", err)
	}
	if data, err := re.chats.AttachmentData(re.ctx, re.user, chat, a.ID); err != nil || len(data) == 0 {
		t.Errorf("data: %v", err)
	}
	other := re.env.user()
	if _, err := re.chats.AttachmentData(re.ctx, other, chat, a.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("another user read it: %v", err)
	}

	// at most 6 wait; unsent ones go after a day, sent ones stay with the chat
	for range maxUnsent {
		if _, err := re.chats.AddAttachment(re.ctx, re.user, chat, photo); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := re.chats.AddAttachment(re.ctx, re.user, chat, photo); !errors.As(err, &ve) {
		t.Errorf("7th waiting photo: %v", err)
	}
	re.clock.add(25 * time.Hour)
	if _, err := re.chats.Cleanup(re.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := re.chats.AddAttachment(re.ctx, re.user, chat, photo); err != nil {
		t.Errorf("after the cleanup: %v", err)
	}
	if _, err := re.chats.AttachmentData(re.ctx, re.user, chat, a.ID); err != nil {
		t.Errorf("the sent photo went with the cleanup: %v", err)
	}
}
