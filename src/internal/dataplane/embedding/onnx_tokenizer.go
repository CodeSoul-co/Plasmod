package embedding

import (
	"bufio"
	"hash/fnv"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// vocabPathEnv is the environment variable for the BERT vocabulary file path.
const vocabPathEnv = "ANDB_ONNX_VOCAB_PATH"

const (
	bertTokCLS = int64(101)
	bertTokSEP = int64(102)
	bertTokPAD = int64(0)
	bertTokUNK = int64(100)

	bertMaxWordLen  = 200
	// bertMaxSubwords caps the number of subword tokens emitted per word.
	// Without this guard, a pathological token that matches many short prefixes
	// causes the O(n²) inner loop in wordPieceSplit to run for the full word
	// length squared.  Exceeding the cap returns [UNK] for the whole word.
	bertMaxSubwords = 200
)

// bertTokenizer implements BERT WordPiece tokenization.
//
// When a vocabulary file is provided (vocab.txt format, one token per line),
// it performs proper WordPiece segmentation with "##" continuation prefixes.
// Without a vocab file it falls back to FNV-32a word hashing that maps each
// whitespace/punct-split word to a deterministic token ID — much better than
// per-character random hashing but still a rough approximation.
//
// Text normalisation pipeline (matches bert-base-uncased):
//  1. Lowercase
//  2. Unicode NFD decomposition
//  3. Strip combining marks (Mn category)
//  4. Clean control characters
//  5. Add whitespace around CJK code points and ASCII punctuation
//  6. Collapse whitespace
type bertTokenizer struct {
	vocab    map[string]int64
	hasVocab bool
}

// newBertTokenizer creates a tokenizer, optionally loading a vocab.txt file.
// Pass "" to use the hash fallback.
func newBertTokenizer(vocabPath string) (*bertTokenizer, error) {
	bt := &bertTokenizer{}
	if vocabPath == "" {
		return bt, nil
	}
	f, err := os.Open(vocabPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	bt.vocab = make(map[string]int64, 32000)
	scanner := bufio.NewScanner(f)
	var id int64
	for scanner.Scan() {
		tok := strings.TrimSpace(scanner.Text())
		if tok != "" {
			bt.vocab[tok] = id
			id++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	bt.hasVocab = len(bt.vocab) > 0
	return bt, nil
}

// tokenize converts text into BERT input_ids and attention_mask of length maxLen.
// [CLS] is prepended and [SEP] appended; the remainder is padded with [PAD].
func (bt *bertTokenizer) tokenize(text string, maxLen int) (ids []int64, mask []int64) {
	ids = make([]int64, maxLen)
	mask = make([]int64, maxLen)

	ids[0] = bertTokCLS
	mask[0] = 1
	pos := 1

	normalized := bt.normalizeText(text)
	words := strings.Fields(normalized)

	for _, word := range words {
		if pos >= maxLen-1 {
			break
		}
		for _, tokID := range bt.wordPieceSplit(word) {
			if pos >= maxLen-1 {
				break
			}
			ids[pos] = tokID
			mask[pos] = 1
			pos++
		}
	}

	ids[pos] = bertTokSEP
	mask[pos] = 1
	return ids, mask
}

// normalizeText applies the bert-base-uncased preprocessing pipeline.
func (bt *bertTokenizer) normalizeText(text string) string {
	text = strings.ToLower(text)

	// NFD decompose then strip combining marks (accents).
	stripped, _, _ := transform.String(
		transform.Chain(norm.NFD, transform.RemoveFunc(func(r rune) bool {
			return unicode.Is(unicode.Mn, r)
		})),
		text,
	)
	text = stripped

	// Clean control chars and add spaces around CJK + punctuation.
	var sb strings.Builder
	sb.Grow(len(text) + 32)
	for _, r := range text {
		if r == 0 || r == 0xFFFD {
			continue
		}
		if isControlRune(r) {
			continue
		}
		if isWhitespaceRune(r) {
			sb.WriteByte(' ')
		} else if isCJKRune(r) || isPunctRune(r) {
			sb.WriteByte(' ')
			sb.WriteRune(r)
			sb.WriteByte(' ')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// wordPieceSplit splits a single (already-normalised) word into token IDs.
func (bt *bertTokenizer) wordPieceSplit(word string) []int64 {
	if !bt.hasVocab {
		// FNV hash fallback: one stable ID per word token.
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		return []int64{int64(h.Sum32()%29000) + 1000}
	}

	chars := []rune(word)
	if len(chars) > bertMaxWordLen {
		return []int64{bertTokUNK}
	}

	subwords := make([]int64, 0, 4)
	start := 0
	for start < len(chars) {
		// Guard against pathological tokens that generate too many subwords,
		// preventing the O(n²) inner loop from becoming a CPU hot-path.
		if len(subwords) >= bertMaxSubwords {
			return []int64{bertTokUNK}
		}
		end := len(chars)
		found := false
		for end > start {
			sub := string(chars[start:end])
			if start > 0 {
				sub = "##" + sub
			}
			if id, ok := bt.vocab[sub]; ok {
				subwords = append(subwords, id)
				start = end
				found = true
				break
			}
			end--
		}
		if !found {
			return []int64{bertTokUNK}
		}
	}
	return subwords
}

func isControlRune(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r)
}

func isWhitespaceRune(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || unicode.Is(unicode.Zs, r)
}

func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F)
}

func isPunctRune(r rune) bool {
	return (r >= 33 && r <= 47) ||
		(r >= 58 && r <= 64) ||
		(r >= 91 && r <= 96) ||
		(r >= 123 && r <= 126) ||
		unicode.Is(unicode.P, r)
}
