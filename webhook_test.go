package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.gh.ink/notifyutils/errors"
	"go.gh.ink/notifyutils/model"
)

type captured struct {
	method string
	body   string
	header http.Header
}

func recorder(status int, body string) (*httptest.Server, *captured) {
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got.method = r.Method
		got.header = r.Header.Clone()
		got.body = string(data)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	return srv, got
}

// clientFor builds a driver client pointed at srv, mirroring what
// client.NewClient does for an application.
func clientFor(t *testing.T, srv *httptest.Server, credential map[string]string) model.Client {
	t.Helper()
	client, err := Driver{}.NewClient(paramsFor(srv, credential))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func paramsFor(srv *httptest.Server, credential map[string]string) model.DriverClientParam {
	if credential == nil {
		credential = map[string]string{}
	}
	credential[URL] = srv.URL
	return model.DriverClientParam{
		Credential: credential,
		HTTPClient: srv.Client(),
		Marshal:    json.Marshal,
		Unmarshal:  json.Unmarshal,
	}
}

func TestName(t *testing.T) {
	if Name != "webhook" {
		t.Errorf("Name = %q, want webhook", Name)
	}
}

func TestNewClientRequiresURL(t *testing.T) {
	_, err := Driver{}.NewClient(model.DriverClientParam{
		Credential: map[string]string{},
		Marshal:    json.Marshal,
		Unmarshal:  json.Unmarshal,
	})

	if !stderrors.Is(err, errors.ErrDriverCredentialInvalid) {
		t.Fatalf("error = %v, want ErrDriverCredentialInvalid", err)
	}
	var typed *errors.NotifyutilsError
	if !stderrors.As(err, &typed) || typed.DriverName() != Name {
		t.Errorf("error should name the driver, got %v", err)
	}
}

func TestNewClientRejectsMalformedHeaders(t *testing.T) {
	srv, _ := recorder(200, "")
	defer srv.Close()

	_, err := Driver{}.NewClient(paramsFor(srv, map[string]string{Headers: "not-json"}))

	if !stderrors.Is(err, errors.ErrDriverCredentialInvalid) {
		t.Fatalf("error = %v, want ErrDriverCredentialInvalid", err)
	}
}

// A malformed template is a configuration error, so it must surface at
// construction rather than on the first send.
func TestNewClientRejectsMalformedTemplate(t *testing.T) {
	srv, _ := recorder(200, "")
	defer srv.Close()

	_, err := Driver{}.NewClient(paramsFor(srv, map[string]string{Template: `{{.NoSuchField`}))

	if !stderrors.Is(err, errors.ErrDriverCredentialInvalid) {
		t.Fatalf("error = %v, want ErrDriverCredentialInvalid", err)
	}
}

func TestSendJSONPayload(t *testing.T) {
	srv, got := recorder(200, "ok")
	defer srv.Close()
	client := clientFor(t, srv, nil)

	err := client.Send(context.Background(), model.Message{
		Title:      "Deploy",
		Text:       "1.4.2 is live",
		Format:     model.FormatMarkdown,
		Level:      model.LevelInfo,
		Recipients: []string{"ops"},
		Vars:       model.Vars{{Key: "host", Value: "web-01"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	for _, want := range []string{
		`"title":"Deploy"`, `"text":"1.4.2 is live"`, `"format":"markdown"`, `"level":"info"`,
		`"recipients":["ops"]`, `"key":"host"`, `"value":"web-01"`,
	} {
		if !strings.Contains(got.body, want) {
			t.Errorf("request body %q missing %s", got.body, want)
		}
	}
	if ct := got.header.Get("Content-Type"); ct != DefaultContentType {
		t.Errorf("Content-Type = %q, want %q", ct, DefaultContentType)
	}
}

func TestSendUsesConfiguredMethodAndHeaders(t *testing.T) {
	srv, got := recorder(201, "")
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{
		Method:  http.MethodPatch,
		Headers: `{"X-Tenant":"acme"}`,
	})

	if err := client.Send(context.Background(), model.Message{Text: "hi"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got.method != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", got.method)
	}
	if got.header.Get("X-Tenant") != "acme" {
		t.Errorf("custom header missing: %v", got.header)
	}
}

func TestSendRendersTemplateWhenConfigured(t *testing.T) {
	srv, got := recorder(200, "")
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{
		Template: `[{{.Level}}] {{.Title}}: {{.Text}}{{range .Vars}} (${{.Key}}={{.Value}}){{end}}`,
	})

	err := client.Send(context.Background(), model.Message{
		Title: "Disk",
		Text:  "92% used",
		Level: model.LevelWarn,
		Vars:  model.Vars{{Key: "host", Value: "db-02"}},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	const want = `[warn] Disk: 92% used ($host=db-02)`
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}
	if ct := got.header.Get("Content-Type"); ct != DefaultTemplateContentType {
		t.Errorf("Content-Type = %q, want the templated default", ct)
	}
}

func TestSendExtraContentTypeOverrides(t *testing.T) {
	srv, got := recorder(200, "")
	defer srv.Close()
	client := clientFor(t, srv, nil)

	err := client.Send(context.Background(), model.Message{
		Text:   "<b>hi</b>",
		Extras: map[string]any{ExtraContentType: "text/html; charset=utf-8"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if ct := got.header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the per-message override", ct)
	}
}

func TestSendSignsBodyWithSecret(t *testing.T) {
	const secret = "s3cr3t"
	srv, got := recorder(200, "")
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{Secret: secret})

	if err := client.Send(context.Background(), model.Message{Text: "signed"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	// Recomputed with a separate HMAC call path so the driver's use of the shared
	// helper is checked against something independent of it.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(got.body))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if gotSig := got.header.Get(DefaultSignHeader); gotSig != want {
		t.Errorf("signature header = %q, want %q", gotSig, want)
	}
}

func TestSendCustomSignHeader(t *testing.T) {
	srv, got := recorder(200, "")
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{Secret: "k", SignHeader: "X-Sig"})

	if err := client.Send(context.Background(), model.Message{Text: "x"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got.header.Get("X-Sig") == "" {
		t.Errorf("expected the signature under the configured header name")
	}
	if got.header.Get(DefaultSignHeader) != "" {
		t.Errorf("signature should not also go to the default header")
	}
}

func TestSendNon2xxFails(t *testing.T) {
	srv, _ := recorder(403, `{"error":"forbidden"}`)
	defer srv.Close()
	client := clientFor(t, srv, nil)

	err := client.Send(context.Background(), model.Message{Text: "x"})
	if !stderrors.Is(err, errors.ErrDriverSendFailed) {
		t.Fatalf("error = %v, want ErrDriverSendFailed", err)
	}
	var typed *errors.NotifyutilsError
	if !stderrors.As(err, &typed) {
		t.Fatalf("error should be a *NotifyutilsError, got %T", err)
	}
	if typed.DriverCode() != "403" {
		t.Errorf("DriverCode() = %q, want 403", typed.DriverCode())
	}
	if !strings.Contains(typed.DriverMessage(), "forbidden") {
		t.Errorf("DriverMessage() = %q, want the endpoint diagnostics", typed.DriverMessage())
	}
}

func TestSendHonoursContext(t *testing.T) {
	srv, _ := recorder(200, "")
	defer srv.Close()
	client := clientFor(t, srv, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := client.Send(ctx, model.Message{Text: "x"}); err == nil {
		t.Errorf("Send() error = nil, want the cancelled context to abort the request")
	}
}

// An endpoint that answers 200 and reports rejection in the body is only caught when
// the driver is told where that code lives.
func TestSendReadsBodyErrorCode(t *testing.T) {
	srv, _ := recorder(200, `{"errcode":310000,"errmsg":"keywords not in content"}`)
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{ErrorCode: "errcode", ErrorMessage: "errmsg"})

	err := client.Send(context.Background(), model.Message{Text: "x"})
	if !stderrors.Is(err, errors.ErrDriverSendFailed) {
		t.Fatalf("error = %v, want ErrDriverSendFailed", err)
	}
	var typed *errors.NotifyutilsError
	if !stderrors.As(err, &typed) {
		t.Fatalf("error should be a *NotifyutilsError, got %T", err)
	}
	// A JSON number must not come back as "310000.000000".
	if typed.DriverCode() != "310000" {
		t.Errorf("DriverCode() = %q, want 310000", typed.DriverCode())
	}
	if typed.DriverMessage() != "keywords not in content" {
		t.Errorf("DriverMessage() = %q", typed.DriverMessage())
	}
}

func TestSendAcceptsEveryZeroShape(t *testing.T) {
	for _, body := range []string{`{"errcode":0}`, `{"errcode":"0"}`, `{"errcode":false}`} {
		srv, _ := recorder(200, body)
		client := clientFor(t, srv, map[string]string{ErrorCode: "errcode"})

		if err := client.Send(context.Background(), model.Message{Text: "x"}); err != nil {
			t.Errorf("body %s: Send() error = %v, want success", body, err)
		}
		srv.Close()
	}
}

func TestSendErrorCodeAbsentIsFailure(t *testing.T) {
	srv, _ := recorder(200, `{"result":"queued"}`)
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{ErrorCode: "errcode"})

	err := client.Send(context.Background(), model.Message{Text: "x"})
	if !stderrors.Is(err, errors.ErrDriverSendFailed) {
		t.Fatalf("error = %v, want ErrDriverSendFailed when the configured key is missing", err)
	}
	// Error() carries the sentinel text only; the reason is on the typed accessor.
	var typed *errors.NotifyutilsError
	if !stderrors.As(err, &typed) {
		t.Fatalf("error should be a *NotifyutilsError, got %T", err)
	}
	if !strings.Contains(typed.DriverMessage(), "errcode") {
		t.Errorf("DriverMessage() = %q, want it to name the missing key", typed.DriverMessage())
	}
}

func TestSendErrorCodeOnNonJSONReply(t *testing.T) {
	srv, _ := recorder(200, `<html>gateway blocked</html>`)
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{ErrorCode: "errcode"})

	if err := client.Send(context.Background(), model.Message{Text: "x"}); !stderrors.Is(err, errors.ErrDriverSendFailed) {
		t.Fatalf("error = %v, want ErrDriverSendFailed", err)
	}
}

// Unconfigured, the driver keeps its documented behaviour of trusting the status line:
// a body code it was never told about cannot be interpreted.
func TestSendIgnoresBodyCodeUntilConfigured(t *testing.T) {
	srv, _ := recorder(200, `{"errcode":310000,"errmsg":"nope"}`)
	defer srv.Close()
	client := clientFor(t, srv, nil)

	if err := client.Send(context.Background(), model.Message{Text: "x"}); err != nil {
		t.Errorf("Send() error = %v, want success without the errorCode credential", err)
	}
}

func TestSendStatusFailureBeatsBodyCode(t *testing.T) {
	srv, _ := recorder(502, `<html>bad gateway</html>`)
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{ErrorCode: "errcode"})

	err := client.Send(context.Background(), model.Message{Text: "x"})
	var typed *errors.NotifyutilsError
	if !stderrors.As(err, &typed) {
		t.Fatalf("error should be a *NotifyutilsError, got %T", err)
	}
	if typed.DriverCode() != "502" {
		t.Errorf("DriverCode() = %q, want the HTTP status", typed.DriverCode())
	}
}

// The template dot is model.Message, which is the documented rendering context for every
// channel in this family: payload mirrors its field names, so the same expressions work,
// while the Extra* helpers only exist on Message.
func TestSendTemplateRendersAgainstMessage(t *testing.T) {
	srv, got := recorder(200, "")
	defer srv.Close()
	client := clientFor(t, srv, map[string]string{
		Template: `{{.Level}}|{{.Title}}|{{index .Recipients 0}}|{{range .Vars}}{{.Key}}={{.Value}};{{end}}|{{.ExtraString "kind"}}`,
	})

	err := client.Send(context.Background(), model.Message{
		Title:      "磁盘",
		Text:       "已用 ${pct}%",
		Level:      model.LevelWarn,
		Format:     model.FormatMarkdown,
		Recipients: []string{"ops@example.net"},
		Vars:       model.Vars{{Key: "pct", Value: "92"}},
		Extras:     map[string]any{"kind": "disk"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	const want = "warn|磁盘|ops@example.net|pct=92;|disk"
	if got.body != want {
		t.Errorf("body = %q, want %q", got.body, want)
	}
}
