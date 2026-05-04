// Command web-chat demonstrates a complete web application using the AI SDK
// UI layer: Templ/Datastar-inspired components, SSE streaming ChatHandler,
// and real-time chat with the OpenAI provider. It starts an HTTP server on
// :8080 with a browser-based chat UI.
//
//	Usage:
//	  OPENAI_API_KEY=sk-... (cd ai-sdk-examples && go run ./web-chat/)
//
//	Open http://localhost:8080 in your browser.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/samcharles93/ai-sdk/pkg/provider/openai"
	"github.com/samcharles93/ai-sdk/pkg/registry"
	"github.com/samcharles93/ai-sdk/pkg/ui/handlers"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	port := flag.Int("port", 8080, "HTTP server port")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY environment variable is required")
	}

	provider, err := openai.New(openai.Config{APIKey: apiKey})
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	reg := registry.New()
	reg.RegisterChat("openai", provider)

	mux := http.NewServeMux()
	mux.HandleFunc("/", serveIndex)
	mux.Handle("/chat", handlers.NewChatHandler(reg, "openai", "gpt-5.4-nano"))

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		logger.Info("server starting", "addr", srv.Addr, "url", fmt.Sprintf("http://localhost:%d", *port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "error", err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

const indexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AI SDK Chat</title>
<style>
  * { margin:0; padding:0; box-sizing:border-box }
  body { font:16px/1.5 system-ui,sans-serif; background:#111; color:#e0e0e0; height:100vh; display:flex; flex-direction:column }
  header { background:#1a1a2e; padding:12px 20px; border-bottom:1px solid #333; display:flex; justify-content:space-between; align-items:center }
  header h1 { font-size:18px; color:#64b5f6 }
  header span { font-size:13px; color:#888 }
  #messages { flex:1; overflow-y:auto; padding:16px 20px }
  .msg { margin-bottom:16px; max-width:75% }
  .msg.user { margin-left:auto; text-align:right }
  .msg.assistant { margin-right:auto }
  .role { font-size:11px; text-transform:uppercase; letter-spacing:0.5px; margin-bottom:4px }
  .msg.user .role { color:#64b5f6 }
  .msg.assistant .role { color:#81c784 }
  .content { padding:10px 14px; border-radius:12px; white-space:pre-wrap; word-wrap:break-word }
  .msg.user .content { background:#1565c0; color:#fff; border-bottom-right-radius:4px }
  .msg.assistant .content { background:#2d2d3d; color:#e0e0e0; border-bottom-left-radius:4px }
  .tool { background:#1b1b2e; border:1px solid #333; border-radius:8px; padding:8px 12px; margin:8px 0; font-size:14px }
  .tool .name { color:#ffb74d; font-weight:600 }
  .tool .result { color:#81c784; margin-top:4px }
  .reasoning { color:#666; font-style:italic; font-size:14px; border-left:2px solid #444; padding-left:10px; margin:8px 0 }
  footer { padding:12px 20px; border-top:1px solid #333; background:#1a1a2e }
  footer form { display:flex; gap:8px }
  footer input { flex:1; padding:10px 14px; border:1px solid #444; border-radius:8px; background:#1e1e30; color:#e0e0e0; font:inherit; outline:none }
  footer input:focus { border-color:#64b5f6 }
  footer button { padding:10px 20px; border:none; border-radius:8px; background:#1565c0; color:#fff; font:inherit; cursor:pointer }
  footer button:hover { background:#1976d2 }
  footer button:disabled { opacity:0.5; cursor:default }
  .status { color:#ffb74d; font-size:13px; padding:4px 0 }
</style>
</head>
<body>
<header>
  <h1>AI SDK Chat</h1>
  <span>Streaming · Reasoning · Tools</span>
</header>
<div id="messages"></div>
<footer>
  <form id="form" autocomplete="off">
    <input type="text" id="input" placeholder="Type a message..." autofocus>
    <button type="submit" id="sendBtn">Send</button>
  </form>
  <div class="status" id="status"></div>
</footer>
<script>
const messagesEl = document.getElementById('messages');
const form = document.getElementById('form');
const input = document.getElementById('input');
const sendBtn = document.getElementById('sendBtn');
const statusEl = document.getElementById('status');

let sessionID = crypto.randomUUID();

form.addEventListener('submit', e => {
  e.preventDefault();
  const text = input.value.trim();
  if (!text) return;
  addMessage('user', text);
  input.value = '';
  sendBtn.disabled = true;
  statusEl.textContent = 'Thinking...';

  const div = document.createElement('div');
  div.className = 'msg assistant';
  div.innerHTML = '<div class="role">assistant</div>';
  const content = document.createElement('div');
  content.className = 'content';
  div.appendChild(content);
  messagesEl.appendChild(div);
  let reasoningEl = null;
  let toolEl = null;

  const url = '/chat?prompt=' + encodeURIComponent(text) + '&sessionID=' + sessionID;
  const es = new EventSource(url);

  es.onmessage = event => {
    const chunk = JSON.parse(event.data);
    switch (chunk.type) {
      case 'text-start':
        break;
      case 'text-delta':
        content.textContent += chunk.delta;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      case 'reasoning-delta':
        if (!reasoningEl) {
          reasoningEl = document.createElement('div');
          reasoningEl.className = 'reasoning';
          div.insertBefore(reasoningEl, content);
        }
        reasoningEl.textContent += chunk.delta;
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      case 'tool-input-available':
        toolEl = document.createElement('div');
        toolEl.className = 'tool';
        toolEl.innerHTML = '<span class="name">🔧 ' + chunk.toolName + '</span>';
        div.appendChild(toolEl);
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      case 'tool-output-available':
        if (toolEl) {
          toolEl.innerHTML += '<div class="result">→ ' + JSON.stringify(chunk.output) + '</div>';
        }
        messagesEl.scrollTop = messagesEl.scrollHeight;
        break;
      case 'finish':
        statusEl.textContent = chunk.usage
          ? '(' + chunk.usage.totalTokens + ' tokens)'
          : '';
        es.close();
        sendBtn.disabled = false;
        break;
      case 'error':
        content.textContent = 'Error: ' + (chunk.errorText || 'unknown');
        statusEl.textContent = '';
        es.close();
        sendBtn.disabled = false;
        break;
    }
  };

  es.onerror = () => {
    if (es.readyState === EventSource.CLOSED) return;
    content.textContent = content.textContent || 'Connection error.';
    statusEl.textContent = '';
    es.close();
    sendBtn.disabled = false;
  };
});

function addMessage(role, text) {
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  div.innerHTML = '<div class="role">' + role + '</div><div class="content">' + text + '</div>';
  messagesEl.appendChild(div);
  messagesEl.scrollTop = messagesEl.scrollHeight;
}
</script>
</body>
</html>`
