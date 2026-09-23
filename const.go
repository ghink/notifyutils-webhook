package webhook

const Name = "webhook"

const URL = "url"
const Method = "method"
const Headers = "headers"
const ContentType = "contentType"
const Secret = "secret"
const SignHeader = "signHeader"
const Template = "template"

// ErrorCode names the JSON key that carries the receiver's own result code. When
// set, a 2xx response is still a failure unless that key holds the zero value: many
// endpoints report rejection in the body while answering 200.
const ErrorCode = "errorCode"

// ErrorMessage names the JSON key holding the human-readable failure text, read only
// when ErrorCode is configured.
const ErrorMessage = "errorMessage"

const DefaultMethod = "POST"
const DefaultContentType = "application/json; charset=utf-8"
const DefaultTemplateContentType = "text/plain; charset=utf-8"
const DefaultSignHeader = "X-Notify-Signature"

// ExtraContentType overrides the Content-Type of one message, for an endpoint that
// expects something other than the configured JSON.
const ExtraContentType = "contentType"
