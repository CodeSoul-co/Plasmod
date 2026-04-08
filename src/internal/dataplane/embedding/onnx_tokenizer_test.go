package embedding

import "testing"

func TestWordPieceSplit_MaxSubwordsGuard(t *testing.T) {
	bt := &bertTokenizer{
		hasVocab: true,
		vocab:    map[string]int64{},
	}
	// Construct a vocab that forces many single-rune matches.
	for i := 0; i < 26; i++ {
		ch := string(rune('a' + i))
		bt.vocab[ch] = int64(1000 + i)
		bt.vocab["##"+ch] = int64(2000 + i)
	}

	// This would produce > bertMaxSubwords subwords with the above vocab.
	longToken := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
	got := bt.wordPieceSplit(longToken)
	if len(got) != 1 || got[0] != bertTokUNK {
		t.Fatalf("expected [UNK] due to max-subwords guard, got=%v", got)
	}
}

