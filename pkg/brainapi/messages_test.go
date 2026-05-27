package brainapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/wh0amibjm/brainapi-go-sdk/pkg/brainapi"
)

func TestMessages(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/self/messages" {
			t.Errorf("wrong path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("type") != "ANNOUNCEMENT" {
			t.Errorf("type param missing: %v", q)
		}
		if q.Get("order") != "-dateCreated" {
			t.Errorf("order param missing: %v", q)
		}
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "messages.json"))
	})
	page, err := cl.Messages(context.Background(), brainapi.ListMessagesOptions{
		Type:  "ANNOUNCEMENT",
		Order: "-dateCreated",
	})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if page.Count != 2 || len(page.Results) != 2 {
		t.Fatalf("wrong page: %+v", page)
	}
	first := page.Results[0]
	if first.ID != "yqpn4PR" || first.Type != "ANNOUNCEMENT" {
		t.Errorf("first message wrong: %+v", first)
	}
	if first.Title == "" || first.DateCreated == "" {
		t.Errorf("title/dateCreated not decoded: %+v", first)
	}
	if first.Read {
		t.Errorf("first message should be unread: %+v", first)
	}
	if page.Results[1].Type != "NOTIFICATION" || !page.Results[1].Read {
		t.Errorf("second message wrong: %+v", page.Results[1])
	}
}

func TestMessagesAll(t *testing.T) {
	t.Parallel()
	_, cl := newTestServerAndClient(t, func(w http.ResponseWriter, _ *http.Request) {
		// Single page (next:null) — iterator must terminate after draining it.
		w.WriteHeader(200)
		_, _ = w.Write(loadFixture(t, "messages.json"))
	})
	out, errs := cl.MessagesAll(context.Background(), brainapi.ListMessagesOptions{})
	var got []brainapi.Message
	for {
		select {
		case m, ok := <-out:
			if !ok {
				out = nil
			} else {
				got = append(got, m)
			}
		case e, ok := <-errs:
			if !ok {
				errs = nil
			} else if e != nil {
				t.Fatalf("MessagesAll: %v", e)
			}
		}
		if out == nil && errs == nil {
			break
		}
	}
	if len(got) != 2 {
		t.Errorf("expected 2 messages drained, got %d", len(got))
	}
}
