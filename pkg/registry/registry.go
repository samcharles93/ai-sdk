package registry

import (
	"errors"
	"sync"

	"github.com/samcharles93/ai-sdk/pkg/agent"
	"github.com/samcharles93/ai-sdk/pkg/chat"
	"github.com/samcharles93/ai-sdk/pkg/embed"
	"github.com/samcharles93/ai-sdk/pkg/image"
	"github.com/samcharles93/ai-sdk/pkg/object"
	"github.com/samcharles93/ai-sdk/pkg/rerank"
	"github.com/samcharles93/ai-sdk/pkg/speech"
	"github.com/samcharles93/ai-sdk/pkg/transcribe"
	"github.com/samcharles93/ai-sdk/pkg/video"
)

// ProviderNotFound is returned when a requested provider is not registered.
var ErrProviderNotFound = errors.New("registry: provider not found")

// Registry manages provider registration and retrieval across all model
// domains (chat, embed, image, rerank, speech, transcription).
type Registry struct {
	mu sync.RWMutex

	chat       map[string]chat.Provider
	embed      map[string]embed.Provider
	img        map[string]image.Provider
	rerankProv map[string]rerank.Provider
	speechProv map[string]speech.Provider
	transcribe map[string]transcribe.Provider
	object     map[string]object.Provider
	video      map[string]video.Provider
	agentProv  map[string]agent.Agent
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{
		chat:       make(map[string]chat.Provider),
		embed:      make(map[string]embed.Provider),
		img:        make(map[string]image.Provider),
		rerankProv: make(map[string]rerank.Provider),
		speechProv: make(map[string]speech.Provider),
		transcribe: make(map[string]transcribe.Provider),
		object:     make(map[string]object.Provider),
		video:      make(map[string]video.Provider),
		agentProv:  make(map[string]agent.Agent),
	}
}

// RegisterChat registers a chat provider under the given name.
func (r *Registry) RegisterChat(name string, p chat.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chat[name] = p
}

// RegisterEmbed registers an embedding provider under the given name.
func (r *Registry) RegisterEmbed(name string, p embed.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embed[name] = p
}

// RegisterImage registers an image provider under the given name.
func (r *Registry) RegisterImage(name string, p image.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.img[name] = p
}

// RegisterRerank registers a reranking provider under the given name.
func (r *Registry) RegisterRerank(name string, p rerank.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rerankProv[name] = p
}

// RegisterSpeech registers a speech provider under the given name.
func (r *Registry) RegisterSpeech(name string, p speech.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.speechProv[name] = p
}

// RegisterTranscribe registers a transcription provider under the given name.
func (r *Registry) RegisterTranscribe(name string, p transcribe.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.transcribe[name] = p
}

// RegisterObject registers an object generation provider under the given name.
func (r *Registry) RegisterObject(name string, p object.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.object[name] = p
}

// RegisterVideo registers a video generation provider under the given name.
func (r *Registry) RegisterVideo(name string, p video.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.video[name] = p
}

// RegisterAgent registers an agent under the given name.
func (r *Registry) RegisterAgent(name string, a agent.Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentProv[name] = a
}

// Chat returns a chat.Client backed by the named provider, or an error if
// not registered.
func (r *Registry) Chat(name string) (*chat.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.chat[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return chat.NewClient(p), nil
}

// Embed returns an embed.Client backed by the named provider, or an error if
// not registered.
func (r *Registry) Embed(name string) (*embed.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.embed[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return embed.NewClient(p), nil
}

// Image returns an image.Client backed by the named provider, or an error if
// not registered.
func (r *Registry) Image(name string) (*image.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.img[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return image.NewClient(p), nil
}

// Rerank returns a rerank.Client backed by the named provider, or an error if
// not registered.
func (r *Registry) Rerank(name string) (*rerank.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.rerankProv[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return rerank.NewClient(p), nil
}

// Speech returns a speech.Client backed by the named provider, or an error if
// not registered.
func (r *Registry) Speech(name string) (*speech.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.speechProv[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return speech.NewClient(p), nil
}

// Transcribe returns a transcribe.Client backed by the named provider, or an
// error if not registered.
func (r *Registry) Transcribe(name string) (*transcribe.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.transcribe[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return transcribe.NewClient(p), nil
}

// Object returns an object.Client backed by the named provider, or an
// error if not registered.
func (r *Registry) Object(name string) (*object.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.object[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return object.NewClient(p), nil
}

// Video returns a video.Client backed by the named provider, or an
// error if not registered.
func (r *Registry) Video(name string) (*video.Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.video[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return video.NewClient(p), nil
}

// Agent returns an agent.Agent registered under the given name, or an
// error if not registered.
func (r *Registry) Agent(name string) (*agent.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agentProv[name]
	if !ok {
		return nil, ErrProviderNotFound
	}
	return &a, nil
}
