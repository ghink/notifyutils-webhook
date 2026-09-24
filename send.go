package webhook

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strconv"

	"go.gh.ink/notifyutils/errors"
	"go.gh.ink/notifyutils/model"
	"go.gh.ink/notifyutils/utils/httpx"
	"go.gh.ink/notifyutils/utils/sign"
)

// Payload is the JSON document an endpoint receives by default. It mirrors the
// Message except that Extras is only present when set, keeping the common body small.
type Payload struct {
	Title      string         `json:"title,omitempty"`
	Text       string         `json:"text"`
	Format     model.Format   `json:"format,omitempty"`
	Level      model.Level    `json:"level,omitempty"`
	Recipients []string       `json:"recipients,omitempty"`
	Template   string         `json:"template,omitempty"`
	Vars       model.Vars     `json:"vars,omitempty"`
	Extras     map[string]any `json:"extras,omitempty"`
}

func (c Client) Send(ctx context.Context, msg model.Message) error {
	payload := Payload{
		Title:      msg.Title,
		Text:       msg.Text,
		Format:     msg.Format,
		Level:      msg.Level,
		Recipients: msg.Recipients,
		Template:   msg.Template,
		Vars:       msg.Vars,
		Extras:     msg.Extras,
	}

	var body []byte
	if c.Body != nil {
		// The dot is model.Message, the one documented rendering context across this
		// family: payload mirrors its field names, so a template written against either
		// reads the same, and Message additionally exposes its Extra* helpers.
		var buf bytes.Buffer
		if err := c.Body.Execute(&buf, msg); err != nil {
			return errors.ErrDriverSendFailed.
				WithDriverName(Name).
				WithDriverMessage("template: " + err.Error())
		}
		body = buf.Bytes()
	} else {
		encoded, err := c.Marshal(payload)
		if err != nil {
			return err
		}
		body = encoded
	}

	contentType := msg.ExtraString(ExtraContentType)
	if contentType == "" {
		contentType = c.ContentType
	}

	header := make(map[string]string, len(c.Headers)+2)
	maps.Copy(header, c.Headers)
	header["Content-Type"] = contentType
	if c.Secret != "" {
		header[c.SignHeader] = "sha256=" + sign.HMACSHA256Hex([]byte(c.Secret), body)
	}

	resp, err := httpx.Do(ctx, c.Client, httpx.Request{
		Method: c.Method,
		URL:    c.URL,
		Header: header,
		Body:   body,
	})
	if err != nil {
		return err
	}

	// Without a configured code key this channel has no agreed response shape, so
	// any 2xx is as much as the driver can tell.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return errors.ErrDriverSendFailed.
			WithDriverName(Name).
			WithDriverCode(strconv.Itoa(resp.StatusCode)).
			WithDriverMessage(string(resp.Body)).
			WithDriverResponse(string(resp.Body))
	}

	if c.ErrorCode != "" {
		return c.verdict(resp)
	}

	return nil
}

// verdict reads the receiver's own result code out of the body, for endpoints that
// answer 200 and report rejection there instead.
func (c Client) verdict(resp *httpx.Response) error {
	var doc map[string]any
	if err := c.Unmarshal(resp.Body, &doc); err != nil {
		return errors.ErrDriverSendFailed.
			WithDriverName(Name).
			WithDriverCode(strconv.Itoa(resp.StatusCode)).
			WithDriverMessage("response is not a JSON object: " + err.Error()).
			WithDriverResponse(string(resp.Body))
	}

	value, ok := doc[c.ErrorCode]
	if !ok {
		return errors.ErrDriverSendFailed.
			WithDriverName(Name).
			WithDriverCode(strconv.Itoa(resp.StatusCode)).
			WithDriverMessage("response has no key " + strconv.Quote(c.ErrorCode)).
			WithDriverResponse(doc)
	}

	if accepted(value) {
		return nil
	}

	message := ""
	if c.ErrorMessage != "" {
		message, _ = doc[c.ErrorMessage].(string)
	}
	if message == "" {
		message = string(resp.Body)
	}

	return errors.ErrDriverSendFailed.
		WithDriverName(Name).
		WithDriverCode(textual(value)).
		WithDriverMessage(message).
		WithDriverResponse(doc)
}

// accepted reports whether a result code means success: the zero of whatever type the
// endpoint uses for it.
func accepted(value any) bool {
	switch typed := value.(type) {
	case bool:
		return !typed
	case float64:
		return typed == 0
	case string:
		return typed == "" || typed == "0"
	default:
		return false
	}
}

// textual renders a result code for DriverCode without the ".0" a JSON number picks up
// when formatted with the default verbs.
func textual(value any) string {
	switch typed := value.(type) {
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}
