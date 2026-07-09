# DeepSeek API Error Codes

Reference for error codes returned by the DeepSeek API. Used to inform the
HTTP status mapping in `deepseek.go`.

| Code | Name                  | Cause                                                   | Solution                                                                 |
|------|-----------------------|---------------------------------------------------------|--------------------------------------------------------------------------|
| 400  | Invalid Format        | Invalid request body format.                            | Fix the body per the error message. See DeepSeek API Docs.              |
| 401  | Authentication Fails  | Wrong API key.                                          | Check / create an API key.                                              |
| 402  | Insufficient Balance  | Account balance exhausted.                              | Top up the account.                                                     |
| 422  | Invalid Parameters    | Request contains invalid parameters.                    | Fix parameters per the error message.                                   |
| 429  | Rate Limit Reached    | Sending requests too quickly.                           | Pace requests; back off / fail over.                                    |
| 500  | Server Error          | DeepSeek-side issue.                                    | Retry after a brief wait.                                               |
| 503  | Server Overloaded     | High traffic.                                           | Retry after a brief wait.                                               |

## SDK mapping (see `deepseek.go`)

- 400 → `chat.ErrInvalidRequest` (or `chat.ErrContextLength` if body mentions context length)
- 401 / 403 → `chat.ErrAuthFailed`
- 402 → `chat.ErrAuthFailed` (treated as a credentials/account problem)
- 422 → `chat.ErrInvalidRequest`
- 429 → `chat.ErrRateLimited`
- 500 / 503 / other 5xx → `chat.ErrProviderUnavailable`
