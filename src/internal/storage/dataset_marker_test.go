package storage

import "testing"

func TestExtractDatasetFileNameFromText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"dataset=deep1B.ibin row=1", "deep1B.ibin"},
		{"hello dataset=GT.public.ibin, row=2", "GT.public.ibin"},
		{"dataset=\"foo.ibin\" something", "foo.ibin"},
		{"dataset=bar.ibin)", "bar.ibin"},
		{"no marker here", ""},
	}
	for _, c := range cases {
		got := ExtractDatasetFileNameFromText(c.in)
		if got != c.want {
			t.Fatalf("input=%q want=%q got=%q", c.in, c.want, got)
		}
	}
}
