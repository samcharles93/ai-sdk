# DeepSeek API Docs

## Your First API Call

The DeepSeek API uses an API format compatible with OpenAI/Anthropic. By modifying the configuration, you can use the OpenAI/Anthropic SDK or softwares compatible with the OpenAI/Anthropic API to access the DeepSeek API.

| PARAM | VALUE |
| base_url (OpenAI) | https://api.deepseek.com |
| base_url (Anthropic)	https://api.deepseek.com/anthropic |
| api_key | apply for an [https://platform.deepseek.com/api_keys](API key) |
| model* | - deepseek-v4-flash |
|        | - deepseek-v4-pro |
|        | - deepseek-chat (to be deprecated on 2026/07/24) |
|        | - deepseek-reasoner (to be deprecated on 2026/07/24) |

* The model names deepseek-chat and deepseek-reasoner will be deprecated on 2026/07/24. For compatibility, they correspond to the non-thinking mode and thinking mode of deepseek-v4-flash, respectively.
Integrate with Agent Tools

The DeepSeek API is supported by many popular AI agent and coding assistant tools. If you use tools like Claude Code, GitHub Copilot, or OpenCode, you can use DeepSeek as the backend model directly — no code required.

See the Agent Integrations Guide (https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code) for details.

## Invoke The Chat API

Once you have obtained an API key, you can access the DeepSeek model using the following example scripts in the OpenAI API format. This is a non-stream example, you can set the stream parameter to true to get stream response.

For examples using the Anthropic API format, please refer to Anthropic API (https://api-docs.deepseek.com/guides/anthropic_api).

```curl
curl https://api.deepseek.com/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${DEEPSEEK_API_KEY}" \
  -d '{
        "model": "deepseek-v4-pro",
        "messages": [
          {"role": "system", "content": "You are a helpful assistant."},
          {"role": "user", "content": "Hello!"}
        ],
        "thinking": {"type": "enabled"},
        "reasoning_effort": "high",
        "stream": false
      }'
```
---

## Rate Limit

DeepSeek API dynamically limits user concurrency based on server load. When you reach the concurrency limit, you will immediately receive an HTTP 429 response.

After your request is sent, it may take some time to receive a response from the server. During this period, your HTTP request will remain connected, and you may continuously receive contents in the following formats:

    Non-streaming requests: Continuously return empty lines
    Streaming requests: Continuously return SSE keep-alive comments (: keep-alive)

These contents do not affect the parsing of the JSON body of the response. If you are parsing the HTTP responses yourself, please ensure to handle these empty lines or comments appropriately.

If the request has not started inference after 10 minutes, the server will close the connection.

## Models & Pricing

The prices listed below are in units of per 1M tokens. A token, the smallest unit of text that the model recognizes, can be a word, a number, or even a punctuation mark. We will bill based on the total number of input and output tokens by the model.
Model Details
MODEL	deepseek-v4-flash(1)	deepseek-v4-pro
BASE URL (OpenAI Format)	https://api.deepseek.com
BASE URL (Anthropic Format)	https://api.deepseek.com/anthropic
MODEL VERSION	DeepSeek-V4-Flash	DeepSeek-V4-Pro
THINKING MODE	Supports both non-thinking and thinking (default) modes
See Thinking Mode for how to switch
CONTEXT LENGTH	1M
MAX OUTPUT	MAXIMUM: 384K
FEATURES	Json Output	✓	✓
Tool Calls	✓	✓
Chat Prefix Completion（Beta）	✓	✓
FIM Completion（Beta）	Non-thinking mode only	Non-thinking mode only
PRICING	1M INPUT TOKENS (CACHE HIT)(2)	$0.0028	$0.003625 (75% off(3))$0.0145
1M INPUT TOKENS (CACHE MISS)	$0.14	$0.435 (75% off(3))$1.74
1M OUTPUT TOKENS	$0.28	$0.87 (75% off(3))$3.48

(1) The model names deepseek-chat and deepseek-reasoner will be deprecated in the future. For compatibility, they correspond to the non-thinking mode and thinking mode of deepseek-v4-flash, respectively.
(2) For all models, the input cache hit price has been reduced to 1/10 of the launch price. This price adjustment takes effect from 2026/4/26 12:15 UTC.
(3) The deepseek-v4-pro model is currently offered at a 75% discount, extended until 2026/05/31 15:59 UTC.
Deduction Rules

The expense = number of tokens × price. The corresponding fees will be directly deducted from your topped-up balance or granted balance, with a preference for using the granted balance first when both balances are available.

Product prices may vary and DeepSeek reserves the right to adjust them. We recommend topping up based on your actual usage and regularly checking this page for the most recent pricing information.

----


## Get User Balance

```go
package main

import (
  "fmt"
  "net/http"
  "io/ioutil"
)

func main() {

  url := "https://api.deepseek.com/user/balance"
  method := "GET"

  client := &http.Client {
  }
  req, err := http.NewRequest(method, url, nil)

  if err != nil {
    fmt.Println(err)
    return
  }
  req.Header.Add("Accept", "application/json")
  req.Header.Add("Authorization", "Bearer <TOKEN>")

  res, err := client.Do(req)
  if err != nil {
    fmt.Println(err)
    return
  }
  defer res.Body.Close()

  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    fmt.Println(err)
    return
  }
  fmt.Println(string(body))
}
```

## List Models

```go
package main

import (
  "fmt"
  "net/http"
  "io/ioutil"
)

func main() {

  url := "https://api.deepseek.com/models"
  method := "GET"

  client := &http.Client {
  }
  req, err := http.NewRequest(method, url, nil)

  if err != nil {
    fmt.Println(err)
    return
  }
  req.Header.Add("Accept", "application/json")
  req.Header.Add("Authorization", "Bearer <TOKEN>")

  res, err := client.Do(req)
  if err != nil {
    fmt.Println(err)
    return
  }
  defer res.Body.Close()

  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    fmt.Println(err)
    return
  }
  fmt.Println(string(body))
}
```

## Create FIM Completion

```go
package main

import (
  "fmt"
  "strings"
  "net/http"
  "io/ioutil"
)

func main() {

  url := "https://api.deepseek.com/beta/completions"
  method := "POST"

  payload := strings.NewReader(`{
  "model": "deepseek-v4-pro",
  "prompt": "Once upon a time, ",
  "echo": false,
  "logprobs": 0,
  "max_tokens": 1024,
  "stop": null,
  "stream": false,
  "stream_options": null,
  "suffix": null,
  "temperature": 1,
  "top_p": 1
}`)

  client := &http.Client {
  }
  req, err := http.NewRequest(method, url, payload)

  if err != nil {
    fmt.Println(err)
    return
  }
  req.Header.Add("Content-Type", "application/json")
  req.Header.Add("Accept", "application/json")
  req.Header.Add("Authorization", "Bearer <TOKEN>")

  res, err := client.Do(req)
  if err != nil {
    fmt.Println(err)
    return
  }
  defer res.Body.Close()

  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    fmt.Println(err)
    return
  }
  fmt.Println(string(body))
}
```

## Create Chat Completion

```go
package main

import (
  "fmt"
  "strings"
  "net/http"
  "io/ioutil"
)

func main() {

  url := "https://api.deepseek.com/chat/completions"
  method := "POST"

  payload := strings.NewReader(`{
  "messages": [
    {
      "content": "You are a helpful assistant",
      "role": "system"
    },
    {
      "content": "Hi",
      "role": "user"
    }
  ],
  "model": "deepseek-v4-pro",
  "thinking": {
    "type": "enabled"
  },
  "reasoning_effort": "high",
  "max_tokens": 4096,
  "response_format": {
    "type": "text"
  },
  "stop": null,
  "stream": false,
  "stream_options": null,
  "temperature": 1,
  "top_p": 1,
  "tools": null,
  "tool_choice": "none",
  "logprobs": false,
  "top_logprobs": null
}`)

  client := &http.Client {
  }
  req, err := http.NewRequest(method, url, payload)

  if err != nil {
    fmt.Println(err)
    return
  }
  req.Header.Add("Content-Type", "application/json")
  req.Header.Add("Accept", "application/json")
  req.Header.Add("Authorization", "Bearer <TOKEN>")

  res, err := client.Do(req)
  if err != nil {
    fmt.Println(err)
    return
  }
  defer res.Body.Close()

  body, err := ioutil.ReadAll(res.Body)
  if err != nil {
    fmt.Println(err)
    return
  }
  fmt.Println(string(body))
}
```

https://api-docs.deepseek.com/guides/tool_calls
https://api-docs.deepseek.com/guides/fim_completion

https://api-docs.deepseek.com/guides/multi_round_chat
https://api-docs.deepseek.com/guides/chat_prefix_completion

https://api-docs.deepseek.com/guides/thinking_mode
