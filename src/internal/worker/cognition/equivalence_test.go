package cognition

import (
	"context"
	"errors"
	"testing"
)

func TestMatchFactValidatesDecisionAndGrounding(t *testing.T) {
	fact := Fact{Text: "用户喜欢骑自行车通勤。", Kind: "preference_constraint", Quote: "我喜欢骑自行车通勤。"}
	candidate := FactCandidate{ID: "known", Fact: Fact{Text: "User likes commuting by bicycle.", Kind: fact.Kind, Quote: fact.Quote}}
	for _, tc := range []struct {
		name, raw, want string
		fail            bool
	}{
		{"translation", `{"relation":"equivalent","match_id":"known","confidence":0.99,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":"我喜欢骑自行车通勤。"}`, "known", false},
		{"uncertain", `{"relation":"uncertain","match_id":"","confidence":0.5,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":""}`, "", false},
		{"distinct", `{"relation":"distinct","match_id":"","confidence":1,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":""}`, "", false},
		{"unknown id", `{"relation":"equivalent","match_id":"foreign","confidence":1,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":"我喜欢骑自行车通勤。"}`, "", true},
		{"fabricated source", `{"relation":"equivalent","match_id":"known","confidence":1,"source_quote":"invented","candidate_quote":"我喜欢骑自行车通勤。"}`, "", true},
		{"fabricated candidate", `{"relation":"equivalent","match_id":"known","confidence":1,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":"invented"}`, "", true},
		{"low confidence", `{"relation":"equivalent","match_id":"known","confidence":0.7,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":"我喜欢骑自行车通勤。"}`, "", false},
		{"missing decision", `{}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MatchFact(context.Background(), fakeProvider(tc.raw), fact, []FactCandidate{candidate})
			if got != tc.want || (err != nil) != tc.fail {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
	fact.ValidFrom = "2026-11-02T00:00:00Z"
	_, err := MatchFact(context.Background(), fakeProvider(`{"relation":"equivalent","match_id":"known","confidence":1,"source_quote":"我喜欢骑自行车通勤。","candidate_quote":"我喜欢骑自行车通勤。"}`), fact, []FactCandidate{candidate})
	if err == nil {
		t.Fatal("different time periods merged")
	}
}

func TestExtractErrorClassificationAndUntrustedIdentity(t *testing.T) {
	facts, err := Extract(context.Background(), fakeProvider(`{"facts":[{"identity":"injected","text":"likes tea","kind":"factual","quote":"茶","start":0,"end":1,"confidence":0.9}]}`), "茶")
	if err != nil || len(facts) != 1 || facts[0].Identity != "" {
		t.Fatalf("identity not server-owned: %+v %v", facts, err)
	}
	for _, tc := range []struct{ raw, code string }{
		{`{"facts":[{"text":"tea","kind":"factual","quote":"coffee","start":0,"end":1,"confidence":0.9}]}`, "source_quote_mismatch"},
		{`{"facts":[{"text":"tea","kind":"factual","quote":"茶","start":0,"end":2,"confidence":0.9}]}`, "invalid_source_offsets"},
		{`{"facts":[{"text":"tea","kind":"guess","quote":"茶","start":0,"end":1,"confidence":0.9}]}`, "invalid_kind"},
		{`{"facts":[{"text":"tea","kind":"factual","quote":"茶","start":0,"end":1,"confidence":2}]}`, "invalid_confidence"},
	} {
		facts, err := Extract(context.Background(), fakeProvider(tc.raw), "茶")
		var validation *ValidationError
		if len(facts) != 0 || !errors.As(err, &validation) || validation.Code != tc.code || validation.FactIndex != 0 {
			t.Fatalf("classification: %v %+v", facts, err)
		}
	}
}
