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
	autoCompactMinMessages           = 4
	estimatedCharsPerToken           = 4
)

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
	// threshold can be computed.
	ContextWindow int
	// ThresholdRatio is the fraction of ContextWindow that triggers
	// compaction. Defaults to 0.75 when unset or out of range.
	ThresholdRatio float64
	// TargetRatio is the fraction of ContextWindow the summary is asked to
	// stay under. Defaults to 0.35 when unset or not below ThresholdRatio.
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
		c.TargetRatio = defaultAutoCompactTargetRatio
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
		if msg.Content == "" {
			// Parts is canonical when set; count its text so multimodal
			// turns are not under-estimated.
			chars += len(msg.Parts.Text())
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

// splitCompactionHistory separates a message slice into the parts that
// survive compaction verbatim and the part that is summarised.
//
// Unlike tau — where the system prompt is a separate field — ai-sdk encodes
// it as a leading RoleSystem message inside the slice, so it is preserved
// explicitly rather than summarised away. A trailing user turn that has not
// yet been answered is also held back, per tau's semantics; the summary is
// inserted ahead of it.
func splitCompactionHistory(messages []chat.Message) (system []chat.Message, current *chat.Message, history []chat.Message) {
	if len(messages) == 0 {
		return nil, nil, nil
	}
	i := 0
	for i < len(messages) && messages[i].Role == chat.RoleSystem {
		i++
	}
	system = messages[:i]

	rest := messages[i:]
	if len(rest) == 0 {
		return system, nil, nil
	}
	if rest[len(rest)-1].Role == chat.RoleUser {
		last := rest[len(rest)-1]
		current = &last
		rest = rest[:len(rest)-1]
	}
	return system, current, rest
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
	provider chat.Provider
	model    string
	tools    core.ToolSet
	cfg      CompactionConfig
	log      *slog.Logger
	usage    chat.Usage
}

// compactorFor returns an OnStep hook for cfg, or nil when compaction is
// disabled. A nil hook avoids the per-step overhead when unused.
func compactorFor(cfg Config, provider chat.Provider, model string, tools core.ToolSet, log *slog.Logger) *compactor {
	if !cfg.Compact.Enabled {
		return nil
	}
	c := &compactor{
		provider: provider,
		model:    model,
		tools:    tools,
		cfg:      cfg.Compact.normalised(),
		log:      log,
	}
	return c
}

func (c *compactor) onStep(ctx context.Context, messages []chat.Message) ([]chat.Message, error) {
	if c.cfg.ContextWindow <= 0 {
		return nil, nil
	}
	if len(messages) < autoCompactMinMessages {
		return nil, nil
	}
	threshold := int(float64(c.cfg.ContextWindow) * c.cfg.ThresholdRatio)
	if estimateRequestTokens(messages, c.tools) < threshold {
		return nil, nil
	}

	system, current, history := splitCompactionHistory(messages)
	if len(history) == 0 {
		return nil, nil
	}

	summary, err := c.summarise(ctx, history, int(float64(c.cfg.ContextWindow)*c.cfg.TargetRatio))
	if err != nil {
		if c.log != nil {
			c.log.Warn("agentloop history compaction failed; preserving original history", "err", err)
		}
		return nil, nil
	}

	replacement := make([]chat.Message, 0, len(system)+2)
	replacement = append(replacement, system...)
	replacement = append(replacement, chat.Message{
		Role:    chat.RoleUser,
		Content: "Conversation summary before auto-compaction:\n\n" + summary,
	})
	if current != nil {
		replacement = append(replacement, *current)
	}

	if c.log != nil {
		c.log.Info("agentloop compacted history",
			"before", len(messages),
			"after", len(replacement),
		)
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

	resp, err := c.provider.Chat(ctx, chat.Request{
		Model:     model,
		Messages:  msgs,
		MaxTokens: targetTokens,
	})
	if err != nil {
		return "", fmt.Errorf("agentloop: compact history: %w", err)
	}
	c.usage = addUsage(c.usage, resp.Usage)
	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return "", errors.New("agentloop: compact history: empty summary")
	}
	return summary, nil
}
