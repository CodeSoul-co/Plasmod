package cognition

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeProvider string

func (p fakeProvider) Complete(context.Context, string, string) (string, error) {
	return string(p), nil
}
func TestExtractValidatesUnicodeSpanAndRejectsNonFacts(t *testing.T) {
	text := "我喜欢茶"
	p := fakeProvider(`{"facts":[{"text":"喜欢茶","kind":"preference_constraint","quote":"喜欢茶","start":1,"end":4,"confidence":0.9}]}`)
	facts, err := Extract(context.Background(), p, text)
	if err != nil || len(facts) != 1 {
		t.Fatalf("valid Unicode evidence: %v", err)
	}
	for _, bad := range []fakeProvider{`{"facts":[{"text":"likes coffee","kind":"factual","quote":"coffee","start":0,"end":4,"confidence":1}]}`, `{"facts":[{"text":"maybe","kind":"assistant_guess","quote":"我喜欢茶","start":0,"end":4,"confidence":0.5}]}`} {
		if _, err := Extract(context.Background(), bad, text); err == nil {
			t.Fatal("invalid evidence accepted")
		}
	}
}
func TestSummaryRejectsInventedCitation(t *testing.T) {
	if _, err := Summarize(context.Background(), fakeProvider(`{"conclusions":[{"text":"claim","source_ids":["unknown"]}]}`), map[string]string{"real": "evidence"}); err == nil {
		t.Fatal("invented citation accepted")
	}
}
func TestHTTPProviderErrorsDoNotLeakProviderBodyOrRetry(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "secret upstream details", 500) }))
	defer s.Close()
	p := &HTTPProvider{BaseURL: s.URL, Model: "test", APIKey: "key", Client: s.Client()}
	_, err := p.Complete(context.Background(), "system", "user")
	if err == nil || err.Error() != "memory provider HTTP 500" || calls != 1 {
		t.Fatalf("bad error contract: %v calls %d", err, calls)
	}
}
