package core

import "errors"

var (
	// ErrNoProvider indicates no chat provider was configured.
	ErrNoProvider = errors.New("core: no chat provider configured")

	// ErrNoOutputGenerated indicates the model produced no output.
	ErrNoOutputGenerated = errors.New("core: no output generated")

	// ErrToolNotFound indicates a tool call referenced a tool not in the tool set.
	ErrToolNotFound = errors.New("core: tool not found")

	// ErrToolExecutionFailed indicates a tool's execute function returned an error.
	ErrToolExecutionFailed = errors.New("core: tool execution failed")

	// ErrToolPanicked indicates a tool's execute function (or a tool
	// lifecycle hook) panicked during execution. The panic is recovered
	// and re-surfaced as a typed [ToolPanicError] carrying the phase
	// (before/during/after) and the recovered value.
	ErrToolPanicked = errors.New("core: tool panicked")

	// ErrMaxStepsReached indicates generation was stopped by a step-count stop condition.
	ErrMaxStepsReached = errors.New("core: max steps reached")

	// ErrAborted indicates the generation was cancelled via context or abort signal.
	ErrAborted = errors.New("core: generation aborted")

	// ErrObjectDecode indicates a provider's structured-output response
	// could not be strictly decoded into the requested Go type, either
	// because the JSON was malformed or because it carried fields the
	// type doesn't declare.
	ErrObjectDecode = errors.New("core: object decode failed")
)
