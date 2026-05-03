package util

import (
	"crypto/rand"
	"fmt"
)

// DefaultIDPrefix is used by [GenerateID].
const DefaultIDPrefix = "ai"

// DefaultIDSize is used by [GenerateID].
const DefaultIDSize = 16

// GenerateID returns a unique identifier with the given prefix and random
// portion length. It uses crypto/rand for randomness and hex encoding.
//
// This is the Go equivalent of the AI SDK's generateId function.
func GenerateID(prefix string, size int) string {
	if prefix == "" {
		prefix = DefaultIDPrefix
	}
	if size <= 0 {
		size = DefaultIDSize
	}
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%x", prefix, b)
}

// IDGenerator is a function that produces unique IDs on each call.
// It is the Go equivalent of the AI SDK's createIdGenerator.
type IDGenerator func() string

// NewIDGenerator returns an IDGenerator that produces IDs with the given
// prefix and size.
func NewIDGenerator(prefix string, size int) IDGenerator {
	return func() string {
		return GenerateID(prefix, size)
	}
}
