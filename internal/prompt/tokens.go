package prompt

import (
	"sync"
	"unicode"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// TokenEstimator abstracts token counting, supporting heuristic and precise implementations.
type TokenEstimator interface {
	Estimate(text string) int
}

// heuristicEstimator uses character-based heuristics: ~4 chars/token for English, ~1.5 chars/token for CJK.
type heuristicEstimator struct{}

func (h heuristicEstimator) Estimate(text string) int {
	if text == "" {
		return 0
	}
	return estimateTokens(text)
}

// tiktokenEstimator uses tiktoken-go BPE tokenization (cl100k_base) for precise counting.
type tiktokenEstimator struct{}

func (t tiktokenEstimator) Estimate(text string) int {
	if text == "" {
		return 0
	}
	return preciseEstimate(text)
}

// estimateTokens uses character heuristics to estimate token count.
// English: ~4 chars/token, CJK: ~1.5 chars/token.
func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	cjkCount := 0
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			cjkCount++
		}
	}
	asciiLen := len(text) - cjkCount*3 // rough: CJK char is ~3 bytes in UTF-8
	if asciiLen < 0 {
		asciiLen = 0
	}
	return (asciiLen + 3) / 4 + (cjkCount*2 + 2) / 3
}

// EstimateTokens exposes the heuristic token estimator to other packages.
// It is suitable for cheap, synchronous pre-flight estimation.
func EstimateTokens(text string) int {
	return estimateTokens(text)
}

// PreciseEstimateTokens exposes the tiktoken BPE estimator to other packages.
// It is more accurate than EstimateTokens but slower and allocates a BPE encoder.
func PreciseEstimateTokens(text string) int {
	return preciseEstimate(text)
}

var (
	encodingOnce sync.Once
	cachedEncoding *tiktoken.Tiktoken
)

// getEncoding lazily initializes and caches the tiktoken encoding.
func getEncoding() *tiktoken.Tiktoken {
	encodingOnce.Do(func() {
		enc, err := tiktoken.EncodingForModel("gpt-4")
		if err == nil {
			cachedEncoding = enc
		}
	})
	return cachedEncoding
}

// preciseEstimate uses tiktoken cl100k_base encoding for exact token counting.
func preciseEstimate(text string) int {
	if text == "" {
		return 0
	}
	enc := getEncoding()
	if enc == nil {
		return estimateTokens(text)
	}
	return len(enc.Encode(text, nil, nil))
}
