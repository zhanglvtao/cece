package prompt

type ContextLayer int

const (
	ContextStable  ContextLayer = iota // 整个会话不变；可缓存
	ContextSession                     // 会话内相对稳定；可缓存
	ContextTurn                        // 每轮重算；不缓存
)

type PromptSegment struct {
	Content string
	Layer   ContextLayer
}

// CacheControl 返回 Anthropic cache_control 值。
// Stable 和 Session 层返回 ephemeral 标记以启用 prompt caching；Turn 层返回 nil。
func (l ContextLayer) CacheControl() map[string]string {
	switch l {
	case ContextStable, ContextSession:
		return map[string]string{"type": "ephemeral"}
	default:
		return nil
	}
}

func (l ContextLayer) String() string {
	switch l {
	case ContextStable:
		return "stable"
	case ContextSession:
		return "session"
	case ContextTurn:
		return "turn"
	default:
		return "unknown"
	}
}

type AssembleResult struct {
	Segments      []PromptSegment
	FullText      string // 拼接纯文本（给非 Anthropic provider 或调试用）
	TokenEstimate int
}
