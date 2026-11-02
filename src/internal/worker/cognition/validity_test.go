package cognition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type validityPromptProvider struct {
	system, input string
}

func (p *validityPromptProvider) Complete(_ context.Context, system, input string) (string, error) {
	p.system, p.input = system, input
	// A grounded fact can retain relative time without inventing an absolute clock.
	data, err := json.Marshal(map[string]any{"facts": []map[string]any{{
		"text": input, "kind": "preference_constraint", "quote": input,
		"start": 0, "end": len([]rune(input)), "confidence": .9,
		"valid_from": "", "valid_to": "",
	}}})
	return string(data), err
}

func TestExtractPromptDeclaresValidityContract(t *testing.T) {
	provider := &validityPromptProvider{}
	source := "今天我偏好清淡的食物。"
	facts, err := Extract(context.Background(), provider, source)
	if err != nil || len(facts) != 1 {
		t.Fatalf("relative-time fact: %+v %v", facts, err)
	}
	for _, required := range []string{"RFC3339", "timezone", "Z", "+08:00", "date-only", "empty strings", "host clock", "midnight", "text", "quote", "strictly earlier"} {
		if !strings.Contains(provider.system, required) {
			t.Errorf("extraction prompt lacks validity requirement %q", required)
		}
	}
	if provider.input != source || facts[0].Text != source || facts[0].Quote != source || facts[0].ValidFrom != "" || facts[0].ValidTo != "" {
		t.Fatalf("relative-time evidence changed: input=%q facts=%+v", provider.input, facts)
	}
}

func TestExtractValidityFormatAndInterval(t *testing.T) {
	for _, tc := range []struct{ name, from, to, code string }{
		{"empty", "", "", ""},
		{"date-only start", "2026-11-02", "", "invalid_validity_time"},
		{"date-only end", "", "2026-11-02", "invalid_validity_time"},
		{"relative start", "today", "", "invalid_validity_time"},
		{"relative end", "", "明天", "invalid_validity_time"},
		{"missing timezone", "2026-11-02T12:00:00", "", "invalid_validity_time"},
		{"UTC", "2026-11-02T04:00:00Z", "", ""},
		{"offset", "2026-11-02T12:00:00+08:00", "", ""},
		{"end only", "", "2026-11-02T12:00:00+08:00", ""},
		{"fractional seconds", "2026-11-02T04:00:00.123Z", "", ""},
		{"interval", "2026-11-02T12:00:00+08:00", "2026-11-03T12:00:00+08:00", ""},
		{"different offsets ordered by instant", "2026-11-02T12:00:00+08:00", "2026-11-02T05:00:00Z", ""},
		{"same instant", "2026-11-02T12:00:00+08:00", "2026-11-02T04:00:00Z", "invalid_validity_interval"},
		{"reversed interval", "2026-11-02T12:00:00+08:00", "2026-11-02T03:00:00Z", "invalid_validity_interval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("我喜欢喝茶。有效期从 %s 到 %s。", tc.from, tc.to)
			data, err := json.Marshal(map[string]any{"facts": []map[string]any{{
				"text": source, "kind": "preference_constraint", "quote": source,
				"start": 0, "end": len([]rune(source)), "confidence": .9,
				"valid_from": tc.from, "valid_to": tc.to,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			facts, err := Extract(context.Background(), fakeProvider(data), source)
			if tc.code != "" {
				var validation *ValidationError
				if !errors.As(err, &validation) || validation.Code != tc.code || validation.FactIndex != 0 || len(facts) != 0 {
					t.Fatalf("invalid time accepted or error lost: facts=%+v error=%v", facts, err)
				}
				return
			}
			if err != nil || len(facts) != 1 || facts[0].ValidFrom != tc.from || facts[0].ValidTo != tc.to || facts[0].Quote != source {
				t.Fatalf("valid evidence changed: facts=%+v error=%v", facts, err)
			}
		})
	}
}
