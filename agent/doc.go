// Package agent provides a tool-loop agent that orchestrates multi-step
// reasoning and tool execution.
//
// It is a higher-level abstraction over [core.StreamText] that presents a
// simplified interface: callers provide a prompt string, tools, and
// configuration, and receive a channel of [StreamEvent] values that
// represent the streamed model output, tool calls, tool results, and
// step boundaries.
//
// The agent owns the tool-loop lifecycle:
//
//  1. It calls [core.StreamText] with the configured tools and options.
//  2. It translates [core.StreamPart] events into [StreamEvent] values on
//     the output channel.
//  3. The underlying tool execution (calling Go functions and feeding
//     results back) is handled by core — the agent concentrates on event
//     translation and lifecycle management.
//
// Usage:
//
//	a := &agent.Agent{
//	    Provider: provider,
//	    Model:    "gpt-5.4",
//	    System:   "You are a helpful assistant.",
//	    Tools: core.ToolSet{
//	        "weather": core.NewTool("weather", "Get weather", schema, executeWeather),
//	    },
//	    MaxSteps: 5,
//	}
//	events, err := a.Run(ctx, "What is the weather in London?")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	for ev := range events {
//	    switch ev.Type {
//	    case agent.EventTextDelta:
//	        fmt.Print(ev.TextDelta)
//	    case agent.EventToolCall:
//	        log.Printf("calling %s: %s", ev.ToolCall.ToolName, ev.ToolCall.Input)
//	    case agent.EventToolResult:
//	        log.Printf("result: %s", ev.ToolResult.Output)
//	    case agent.EventFinish:
//	        log.Printf("done: %s", ev.FinishReason)
//	    }
//	}
package agent
