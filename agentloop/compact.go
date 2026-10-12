package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/samcharles93/ai-sdk/chat"
	"github.com/samcharles93/ai-sdk/core"
)

const (
	defaultAutoCompactThresholdRatio = 0.75
	defaultAutoCompactTargetRatio    = 0.35
	estimatedCharsPerToken           = 4
)

// ErrContextLimit means the preserved context cannot fit the configured window.
var ErrContextLimit = errors.New("agentloop: context window exceeded")

// CompactionConfig bounds automatic history compaction. When enabled, a run
// whose accumulated history approaches the model's context window summarises
// older turns into a compact continuation summary instead of exhausting the
// budget. This is the mechanism tau's coordinator uses to let a long run
// continue rather than die at the context limit.
type CompactionConfig struct {
	// Enabled turns compaction on.
	Enabled bool
	// ContextWindow is the model's context window in tokens. It is required
	// for the character-based estimate; zero disables compaction because no
	// threshold can be computed. Ten percent is reserved for model output;
	// input sizes use a character-based estimate.
	ContextWindow int
	// ThresholdRatio is the fraction of ContextWindow that triggers
	// compaction. Defaults to 0.75 when unset or out of range.
	ThresholdRatio float64
	// TargetRatio is the fraction of ContextWindow the summary is asked to
	// stay under, further limited by retained context. Defaults to the smaller
	// of 0.35 and half ThresholdRatio when unset or out of range.
	TargetRatio float64
	// Model, when non-empty, names the model used for the summarisation
	// call. Empty uses the run's model.
	Model string
}

func (c CompactionConfig) normalised() CompactionConfig {
	if c.ThresholdRatio <= 0 || c.ThresholdRatio >= 1 {
		c.ThresholdRatio = defaultAutoCompactThresholdRatio
	}
	if c.TargetRatio <= 0 || c.TargetRatio >= c.ThresholdRatio {
		c.TargetRatio = min(defaultAutoCompactTargetRatio, c.ThresholdRatio/2)
	}
	return c
}

// estimateRequestTokens is a cheap pre-flight size estimate: characters over
// an assumed chars-per-token ratio, plus a per-message overhead. It is a
// threshold, not accounting — deliberately avoiding a real tokenizer.
func estimateRequestTokens(messages []chat.Message, tools core.ToolSet) int {
	chars := 0
	for _, msg := range messages {
		chars += len(msg.Content)
		chars += len(msg.ToolCallID)
		for _, call := range msg.ToolCalls {
			chars += len(call.ID)
			chars += len(call.Name)
			chars += len(call.Arguments)
		}
		for _, part := range msg.Parts {
			switch p := part.(type) {
			case chat.TextPart:
				if msg.Content == "" {
					chars += len(p.Text)
				}
			case chat.ReasoningPart:
				// Replayed thinking is sent back on the wire but never
				// appears in Content.
				chars += len(p.Text)
			}
		}
	}
	for name, tool := range tools {
		chars += len(name)
		chars += len(tool.Description)
		chars += len(tool.Parameters)
	}
	tokens := (chars + estimatedCharsPerToken - 1) / estimatedCharsPerToken
	tokens += len(messages) * 4
	if tokens < 0 {
		return 0
	}
	return tokens
}

// splitCompactionHistory preserves the initial context and the last two
// assistant turns with all their tool results. Boundaries never split a batch.
func splitCompactionHistory(messages []chat.Message) (prefix, history, recent []chat.Message) {
	i := 0
	for i < len(messages) && messages[i].Role == chat.RoleSystem {
		i++
	}
	if i < len(messages) && messages[i].Role == chat.RoleUser {
		i++
	}
	start, turns := len(messages), 0
	for j := len(messages) - 1; j >= i; j-- {
		if messages[j].Role == chat.RoleAssistant {
			start = j
			turns++
			if turns == 2 {
				break
			}
		}
	}
	return messages[:i], messages[i:start], messages[start:]
}

// compactionInstruction asks the model to compress the history. A non-zero
// target adds a token budget so the summary itself stays bounded.
func compactionInstruction(targetTokens int) string {
	if targetTokens <= 0 {
		return "Compact the conversation history above into a dense continuation summary."
	}
	return fmt.Sprintf(
		"Compact the conversation history above into a dense continuation summary under roughly %d tokens.",
		targetTokens,
	)
}

// compactor adapts CompactionConfig to the core.GenerateOptions.OnStep seam.
// It summarises the history via the provider when the token estimate crosses
// the threshold.
type compactor struct {
	provider   chat.Provider
	model      string
	modelInfo  chat.ModelInfo
	tools      core.ToolSet
	cfg        CompactionConfig
	log        *slog.Logger
	usage      chat.Usage
	modelHooks []core.ModelHook
}

// compactorFor returns an OnStep hook for cfg, or nil when compaction is
// disabled. A nil hook avoids the per-step overhead when unused. info is the
// run model's request-shaping flags; when an override model is configured its
// own flags are resolved instead so a reasoning summariser gets the right
// parameter name.
func compactorFor(cfg Config, provider chat.Provider, model string, info chat.ModelInfo, tools core.ToolSet, log *slog.Logger) *compactor {
	if !cfg.Compact.Enabled {
		return nil
	}
	c := &compactor{
		provider:   provider,
		model:      model,
		modelInfo:  info,
		tools:      tools,
		cfg:        cfg.Compact.normalised(),
		log:        log,
		modelHooks: cfg.ModelHooks,
	}
	if override := c.cfg.Model; override != "" && override != model && cfg.Runtime != nil && cfg.ModelRef != "" {
		if ref, err := cfg.Runtime.ParseModelRef(cfg.ModelRef); err == nil {
			if overrideInfo, err := cfg.Runtime.ChatModelInfo(ref.ProviderID + "/" + override); err == nil {
				c.modelInfo = overrideInfo
			}
		}
	}
	return c
}

func (c *compactor) outputReserve() int {
	if c == nil || c.cfg.ContextWindow <= 0 {
		return 0
	}
	return max(1, c.cfg.ContextWindow/10)
}

func (c *compactor) checkSize(messages []chat.Message) error {
	if c == nil || c.cfg.ContextWindow <= 0 {
		return nil
	}
	limit := c.cfg.ContextWindow - c.outputReserve()
	if n := estimateRequestTokens(messages, c.tools); n > limit {
		return fmt.Errorf("%w: estimated input %d exceeds %d tokens after output reserve", ErrContextLimit, n, limit)
	}
	return nil
}

func (c *compactor) onStep(ctx context.Context, messages []chat.Message) ([]chat.Message, error) {
	if c.cfg.ContextWindow <= 0 {
		return nil, nil
	}
	threshold := int(float64(c.cfg.ContextWindow) * c.cfg.ThresholdRatio)
	if estimateRequestTokens(messages, c.tools) < threshold {
		return nil, c.checkSize(messages)
	}

	prefix, history, recent := splitCompactionHistory(messages)
	// Retain at least the newest complete turn; evict the older retained turn
	// if it would leave no useful space for the summary.
	limit := c.cfg.ContextWindow - c.outputReserve()
	for len(recent) > 0 && estimateRequestTokens(recent, nil) > limit/2 {
		next := 1
		for next < len(recent) && recent[next].Role != chat.RoleAssistant {
			next++
		}
		if next == len(recent) {
			break
		}
		history = messages[len(prefix) : len(prefix)+len(history)+next]
		recent = recent[next:]
	}
	if len(history) == 0 {
		return nil, c.checkSize(messages)
	}

	replacement := append([]chat.Message(nil), prefix...)
	replacement = append(replacement, chat.Message{Role: chat.RoleUser, Content: "Conversation summary before auto-compaction:\n\n"})
	summaryIndex := len(replacement) - 1
	replacement = append(replacement, recent...)
	target := min(int(float64(c.cfg.ContextWindow)*c.cfg.TargetRatio), limit-estimateRequestTokens(replacement, c.tools))
	if target <= 0 {
		return nil, c.checkSize(messages)
	}

	summary, err := c.summarise(ctx, history, target)
	if err != nil {
		if c.log != nil {
			c.log.Warn("agentloop history compaction failed; preserving original history", "err", err)
		}
		if sizeErr := c.checkSize(messages); sizeErr != nil {
			return nil, fmt.Errorf("%w: compaction failed: %v", sizeErr, err)
		}
		return nil, nil
	}
	replacement[summaryIndex].Content += summary
	if err := c.checkSize(replacement); err != nil {
		if c.checkSize(messages) == nil {
			return nil, nil
		}
		return nil, err
	}
	if c.log != nil {
		c.log.Info("agentloop compacted history", "before", len(messages), "after", len(replacement))
	}
	return replacement, nil
}

// summarise asks the model for a dense continuation summary of history. It is
// a separate, tool-free call using the compactor's model (or an override).
func (c *compactor) summarise(ctx context.Context, history []chat.Message, targetTokens int) (string, error) {
	model := c.model
	if c.cfg.Model != "" {
		model = c.cfg.Model
	}

	msgs := make([]chat.Message, 0, len(history)+2)
	msgs = append(msgs, chat.Message{
		Role:    chat.RoleSystem,
		Content: "You are a conversation summariser. Produce a dense, faithful summary that preserves every decision, fact, and outstanding task.",
	})
	msgs = append(msgs, history...)
	msgs = append(msgs, chat.Message{Role: chat.RoleUser, Content: compactionInstruction(targetTokens)})

	targetTokens = min(targetTokens, c.cfg.ContextWindow-estimateRequestTokens(msgs, nil))
	if targetTokens <= 0 {
		return "", fmt.Errorf("%w: summarisation input leaves no output space", ErrContextLimit)
	}
	msgs[len(msgs)-1].Content = compactionInstruction(targetTokens)
	resp, err := core.GenerateText(ctx, c.provider, core.GenerateOptions{
		Model:      model,
		Messages:   msgs,
		MaxTokens:  targetTokens,
		ModelInfo:  c.modelInfo,
		ModelHooks: c.modelHooks,
		MaxSteps:   1,
	})
	if err != nil {
		return "", fmt.Errorf("agentloop: compact history: %w", err)
	}
	c.usage = addUsage(c.usage, resp.TotalUsage)
	summary := strings.TrimSpace(resp.Text)
	if summary == "" {
		return "", errors.New("agentloop: compact history: empty summary")
	}
	return summary, nil
}
