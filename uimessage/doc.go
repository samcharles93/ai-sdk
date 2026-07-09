// Package uimessage implements the AI SDK UI Message Stream protocol.
//
// It is a Go port of @ai-sdk/ai's "ui" + "ui-message-stream" subpackages.
// The package stands alone from any specific UI framework: it defines the
// wire protocol, the message + part data types, and a stream processor
// (reducer) that builds an evolving Message from a sequence of Chunks.
package uimessage
