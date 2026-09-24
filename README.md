# notifyutils-webhook

Generic HTTP webhook driver for [notifyutils](../notifyutils).

Delivers a notification to any endpoint that accepts an HTTP request: an internal alert
router, a CI hook, a gateway that fans out to something else. Unlike the channel-specific
drivers there is no vendor API to match, so this module defines the body it sends and lets you
replace it.

## Import

```bash
go get go.gh.ink/notifyutils/webhook
```

```go
import _ "go.gh.ink/notifyutils/webhook"   // registers the driver as "webhook"
```

## Credentials

| Key | Required | Default | Meaning |
|-----|----------|---------|---------|
| `url` | yes | — | Endpoint to POST to |
| `method` | no | `POST` | Any verb the endpoint expects |
| `headers` | no | — | JSON object of string headers, e.g. `{"Authorization":"Bearer …"}` |
| `contentType` | no | `application/json; charset=utf-8` | Sent as `Content-Type`; with `template` set it defaults to `text/plain; charset=utf-8` |
| `secret` | no | — | Enables request signing (see below) |
| `signHeader` | no | `X-Notify-Signature` | Header the signature is written to |
| `template` | no | — | Go `text/template` for the body, executed against `webhook.Payload` |
| `errorCode` | no | — | JSON key the receiver puts its own result code in; see [Success](#success) |
| `errorMessage` | no | — | JSON key holding the failure text, read only when `errorCode` is set |

`headers` is parsed at construction; malformed JSON fails with
`errors.ErrDriverCredentialInvalid`. So is `template` — a template that cannot be parsed is a
configuration error and never reaches the network.

## Body

Without a `template` the body is the JSON form of the message:

```json
{
  "title": "Deploy finished",
  "text": "release 1.4.2 is live on web-01",
  "format": "markdown",
  "level": "info",
  "recipients": ["ops"],
  "vars": [{ "key": "version", "value": "1.4.2" }, { "key": "host", "value": "web-01" }]
}
```

`text` and `title` arrive **already rendered**: the core binds `${key}` placeholders before any
driver sees a message, so the `vars` array is the same data in structured form, not something the
receiver needs for substitution. Use it to key your own template, or ignore it.

`title`, `format`, `level`, `recipients`, `template` and `vars` are omitted when empty, and
`extras` is included only when the message carries some. The Go type behind this shape is
exported as `webhook.Payload`; a `template` is executed against **`model.Message`** instead,
whose field names match one for one, so `{{.Title}}`, `{{.Text}}`, `{{.Level}}`,
`{{.Recipients}}` and `{{range .Vars}}{{.Key}}{{.Value}}{{end}}` read the same either way.
`model.Message` additionally answers `{{.ExtraString "k"}}` and its sibling helpers:

```go
credential := map[string]string{
	webhook.URL:      "https://internal.example.com/alerts",
	webhook.Method:   "POST",
	webhook.Headers:  `{"Authorization":"Bearer …"}`,
	webhook.Template: `[{{.Level}}] {{.Title}}: {{.Text}}{{range .Vars}} (${{.Key}}=${.Value}){{end}}`,
}
```

Because `Extras` is `map[string]any` with driver-defined contents, a template that walks it is
coupled to whatever the sending code put there; prefer the typed fields.

Text inside a template is inserted **raw**: the driver does not quote or escape what `{{...}}`
produces, so a body meant to be JSON either has to keep interpolated values free of quotes and
newlines, or build the document out of fields the endpoint tolerates. `notifyutils-lark` offers a
`{{json …}}` helper for exactly that case; a generic endpoint usually renders plain text or a
form body and needs nothing of the kind.

## Signing

When `secret` is set the request carries

```
X-Notify-Signature: sha256=<hex over the exact request body>
```

an HMAC-SHA256 keyed by `secret` over the bytes actually sent. This is a notifyutils convention
for this driver — the same shape GitHub webhooks use, so many receivers verify it as is — and the
header name is yours to change with `signHeader`.

## Success

By default any 2xx status is delivery and every other status is
`errors.ErrDriverSendFailed`, carrying the status code in `DriverCode()` and the response body in
`DriverMessage()`/`DriverResponse()`.

That default is not enough for an endpoint which answers **200 and reports rejection in the
body** — several robot and gateway APIs do exactly that. Point `errorCode` at the key holding the
result code and the driver fails unless it carries the zero of whatever type the endpoint uses for
it: `0`, `"0"`, `false`, or an empty string. The value becomes `DriverCode()` (formatted without
the `.0` a JSON number would otherwise pick up) and `errorMessage`'s key supplies `DriverMessage()`
falling back to the whole body. A missing key or a non-JSON body is itself a failure, so a
mis-set `errorCode` surfaces loudly instead of silently passing everything through.

```go
credential := map[string]string{
	webhook.URL:          "https://oapi.example.com/robot/send?access_token=…",
	webhook.ErrorCode:    "errcode",
	webhook.ErrorMessage: "errmsg",
}
```

This driver has no vendor contract to consult, since the endpoint is yours: it cannot know whether
`{"status":"ok"}` means success, so it only judges codes it was told to read. `msg.Format`,
`msg.Level` and `msg.Vars` are forwarded rather than interpreted — rendering is the receiver's
business — and `Recipients` travels as data, not as a target, because the target is the URL.

## Extras

| Key | Go type | Effect |
|-----|---------|--------|
| `webhook.ExtraContentType` | `string` | Overrides `Content-Type` for one message, for a receiver that wants XML or form data |

## Errors

| Situation | Error |
|-----------|-------|
| `url` missing, or `headers`/`template` unparsable | `errors.ErrDriverCredentialInvalid` with `DriverName() == "webhook"` |
| non-2xx response | `errors.ErrDriverSendFailed` with the status code and response body |
| `errorCode` configured and the body says no, lacks that key, or is not a JSON object | `errors.ErrDriverSendFailed` with `DriverCode()` from the body (the HTTP status when it cannot be read) |
| transport failure, expired context | the underlying `net/http` error, unwrapped |

`Error()` reports the sentinel text alone, as it does across this family; the reason, code and
body come from the `DriverMessage()`, `DriverCode()` and `DriverResponse()` accessors after an
`errors.As`.

## Requirements

Go 1.26.0+. Dependencies: the Go standard library plus `go.gh.ink/notifyutils`.

## License

[Apache License 2.0](LICENSE)
