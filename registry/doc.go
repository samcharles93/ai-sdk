// Package registry provides a provider registry for managing multiple
// AI model providers (chat, embedding, image, speech, transcription)
// through a single, unified interface.
//
// This is the Go equivalent of the AI SDK's createProviderRegistry function.
//
// Usage:
//
//	reg := registry.New()
//	reg.RegisterChat("openai", openaiProvider)
//	reg.RegisterEmbed("openai", openaiEmbedProvider)
//
//	chatClient := reg.Chat("openai")
//	embedClient := reg.Embed("openai")
package registry
