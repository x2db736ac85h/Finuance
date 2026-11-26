package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/x2db736ac85h/finuance/backend/internal/llm"
	"github.com/x2db736ac85h/finuance/backend/internal/platform"
	"github.com/x2db736ac85h/finuance/backend/testutil/llmmock"
)

// A question asked again or edited (Phase 27): it, its answer and everything after leave the
// chat, the model no longer sees them, and the question's photos can go with the new one.
func TestReplaceFrom(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	questions := func() []string {
		t.Helper()
		_, msgs, err := re.chats.Get(re.ctx, re.user, chat)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, m := range msgs {
			if m.Role == llm.RoleUser {
				out = append(out, m.Content)
			}
		}
		return out
	}
	at := func(n int) func(*Request) { return func(r *Request) { r.ReplaceFrom = &n } }

	re.mock.Push(llmmock.Reply{Content: "One."}, llmmock.Reply{Content: "Two."}, llmmock.Reply{Content: "Three."})
	re.ask(chat, "First question", nil)
	re.ask(chat, "Second question", nil)
	re.ask(chat, "Third question", nil)
	if got := questions(); len(got) != 3 {
		t.Fatalf("questions = %q", got)
	}

	// the second question edited: it and the third go, the first stays
	re.mock.Push(llmmock.Reply{Content: "Two again."})
	evs := re.ask(chat, "Second question, edited", at(1))
	if last(evs).Name != "done" {
		t.Fatalf("events = %s", names(evs))
	}
	if got := strings.Join(questions(), " | "); got != "First question | Second question, edited" {
		t.Errorf("questions after the edit = %q", got)
	}
	// the model was shown the first turn and the new question, nothing of what was removed
	reqs := re.mock.Requests()
	sent := string(reqs[len(reqs)-1])
	if !strings.Contains(sent, "First question") || !strings.Contains(sent, "Second question, edited") ||
		strings.Contains(sent, "Third question") || strings.Contains(sent, "Two.") || strings.Contains(sent, "Three.") {
		t.Errorf("the model's history after the edit: %s", sent)
	}

	// the number of questions itself: nothing to remove, the question is added
	re.mock.Push(llmmock.Reply{Content: "Three again."})
	re.ask(chat, "A new third", at(2))
	if got := questions(); len(got) != 3 || got[2] != "A new third" {
		t.Errorf("questions = %q", got)
	}

	// beyond the end: refused, and the chat is as it was
	var ve *platform.ValidationError
	n := 7
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "Nowhere", Connection: "mock", Model: "mock-tools",
		ReplaceFrom: &n}); !errors.As(err, &ve) || !strings.Contains(err.Error(), "3 questions") {
		t.Errorf("beyond the end: %v", err)
	}
	// a run that is refused for another reason removes nothing either
	zero := 0
	if _, err := re.runner.Prepare(re.ctx, re.user, Request{ChatID: chat, Text: "x", Connection: "mock", Model: "mock-tools",
		ReplaceFrom: &zero, Mode: "nonsense"}); !errors.As(err, &ve) {
		t.Errorf("a bad request: %v", err)
	}
	if got := questions(); len(got) != 3 {
		t.Errorf("a refused run changed the chat: %q", got)
	}

	// from the first question: the chat starts over
	re.mock.Push(llmmock.Reply{Content: "Fresh."})
	re.ask(chat, "Only question", at(0))
	if got := strings.Join(questions(), " | "); got != "Only question" {
		t.Errorf("questions after starting over = %q", got)
	}
}

// The photos of a question that is asked again go with it again; those of later questions go.
func TestReplaceFromWithPhotos(t *testing.T) {
	re := newRunEnv(t, 2)
	chat := re.newChat()
	first, err := re.chats.AddAttachment(re.ctx, re.user, chat, testPhoto(t))
	if err != nil {
		t.Fatal(err)
	}
	re.mock.Push(llmmock.Reply{Content: "A receipt."})
	re.ask(chat, "What is this?", func(r *Request) { r.Model, r.AttachmentIDs = "mock-vl", []uuid.UUID{first.ID} })
	later, err := re.chats.AddAttachment(re.ctx, re.user, chat, testPhoto(t))
	if err != nil {
		t.Fatal(err)
	}
	re.mock.Push(llmmock.Reply{Content: "Another."})
	re.ask(chat, "And this?", func(r *Request) { r.Model, r.AttachmentIDs = "mock-vl", []uuid.UUID{later.ID} })

	// the first question again, with its photo: accepted (a sent photo is refused otherwise)
	zero := 0
	re.mock.Push(llmmock.Reply{Content: "Still a receipt."})
	evs := re.ask(chat, "What is this, exactly?", func(r *Request) {
		r.Model, r.AttachmentIDs, r.ReplaceFrom = "mock-vl", []uuid.UUID{first.ID}, &zero
	})
	if last(evs).Name != "done" {
		t.Fatalf("events = %s", names(evs))
	}
	_, msgs, err := re.chats.Get(re.ctx, re.user, chat)
	if err != nil {
		t.Fatal(err)
	}
	var shown []uuid.UUID
	for _, m := range msgs {
		for _, a := range m.Attachments {
			shown = append(shown, a.ID)
		}
	}
	if len(shown) != 1 || shown[0] != first.ID {
		t.Errorf("photos in the chat = %v, want only the first question's", shown)
	}
	// the later question's photo went with it
	if _, err := re.chats.AttachmentData(re.ctx, re.user, chat, later.ID); !errors.Is(err, platform.ErrNotFound) {
		t.Errorf("the removed question's photo: %v", err)
	}
}

// With the thinking level locked by the administrator (Phase 27), what a question asks for is
// ignored: the model's default applies.
func TestLockThinking(t *testing.T) {
	re := newRunEnv(t, 2)
	// a Qwen 3 name: thinking off is sent as "/no_think" after the question
	re.mock.Models = append(re.mock.Models, "qwen3-mock")
	off := func(r *Request) { r.Model, r.Think = "qwen3-mock", llm.ThinkOff }
	sentNoThink := func() bool {
		reqs := re.mock.Requests()
		return strings.Contains(string(reqs[len(reqs)-1]), "/no_think")
	}
	re.mock.Push(llmmock.Reply{Content: "Hello."})
	re.ask(re.newChat(), "Hi", off)
	if !sentNoThink() {
		t.Fatal("thinking off is not sent as the switch; the test's model changed")
	}
	re.runner.cfg.Choice = func(context.Context) Choice { return Choice{People: true, LockThinking: true} }
	re.mock.Push(llmmock.Reply{Content: "Hello."})
	re.ask(re.newChat(), "Hi", off)
	if sentNoThink() {
		t.Error("a locked thinking level still followed the question's choice")
	}
}
