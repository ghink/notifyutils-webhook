package webhook

import (
	"net/http"
	"text/template"

	"go.gh.ink/notifyutils/errors"
	"go.gh.ink/notifyutils/model"
)

type Client struct {
	URL         string
	Method      string
	Headers     map[string]string
	ContentType string
	Secret      string
	SignHeader  string
	// ErrorCode and ErrorMessage name the body keys a receiver reports its own
	// verdict in. An empty ErrorCode leaves success as "any 2xx".
	ErrorCode    string
	ErrorMessage string
	// Body is set only when the credentials carry a template: the payload is then
	// rendered through it instead of being sent as JSON.
	Body   *template.Template
	Client *http.Client
	// JSON
	Marshal   func(any) ([]byte, error)
	Unmarshal func([]byte, any) error
}

type Driver struct{}

func (d Driver) NewClient(params model.DriverClientParam) (model.Client, error) {
	url := params.Credential[URL]
	if url == "" {
		return Client{}, errors.ErrDriverCredentialInvalid.WithDriverName(Name)
	}

	headers := make(map[string]string)
	if raw := params.Credential[Headers]; raw != "" {
		if err := params.Unmarshal([]byte(raw), &headers); err != nil {
			return nil, errors.ErrDriverCredentialInvalid.
				WithDriverName(Name).
				WithDriverMessage("headers must be a JSON object of strings").
				WithDriverResponse(raw)
		}
	}

	method := params.Credential[Method]
	if method == "" {
		method = DefaultMethod
	}

	// A templated body is text by nature, so it changes the default content type.
	contentType := params.Credential[ContentType]

	var body *template.Template
	if raw := params.Credential[Template]; raw != "" {
		parsed, err := template.New(Name).Parse(raw)
		if err != nil {
			return nil, errors.ErrDriverCredentialInvalid.
				WithDriverName(Name).
				WithDriverMessage("template: " + err.Error())
		}
		body = parsed
		if contentType == "" {
			contentType = DefaultTemplateContentType
		}
	}
	if contentType == "" {
		contentType = DefaultContentType
	}

	signHeader := params.Credential[SignHeader]
	if signHeader == "" {
		signHeader = DefaultSignHeader
	}

	return Client{
		URL:          url,
		Method:       method,
		Headers:      headers,
		ContentType:  contentType,
		Secret:       params.Credential[Secret],
		SignHeader:   signHeader,
		ErrorCode:    params.Credential[ErrorCode],
		ErrorMessage: params.Credential[ErrorMessage],
		Body:         body,
		Client:       params.HTTPClient,
		Marshal:      params.Marshal,
		Unmarshal:    params.Unmarshal,
	}, nil
}
