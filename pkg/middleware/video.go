package middleware

import "github.com/samcharles93/ai-sdk/pkg/video"

// VideoMiddleware wraps a video.Provider to intercept and potentially
// modify calls. Middleware can be stacked to compose behaviour.
type VideoMiddleware func(video.Provider) video.Provider

// ChainVideo composes multiple VideoMiddleware into a single middleware.
// It uses the generic Chain function from chain.go.
func ChainVideo(ms ...VideoMiddleware) VideoMiddleware {
	return ChainGeneric[video.Provider, VideoMiddleware](ms...)
}
