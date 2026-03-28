package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// httpDo performs an HTTP request and decodes the JSON response into dest.
// It wraps CogDB error responses into SDKError.
func httpDo(ctx context.Context, client *http.Client, method string, u *url.URL, body, dest any) error {
	var bodyReader io.Reader
	if body != nil {
		bts, err := json.Marshal(body)
		if err != nil {
			return newError(strings.ToLower(method)+"_marshal", err, "")
		}
		bodyReader = bytes.NewReader(bts)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return newError(strings.ToLower(method)+"_request", err, u.String())
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return newError(strings.ToLower(method)+"_request", ErrCogDBUnavailable, err.Error())
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return newError(strings.ToLower(method)+"_request", ErrCogDBUnavailable,
			u.Path+" returned status "+resp.Status)
	}

	if dest != nil {
		if err := decodeJSON(resp.Body, dest); err != nil {
			return newError("decode_response", ErrInvalidResponse, err.Error())
		}
	}
	return nil
}

// decodeJSON reads the entire body and unmarshals it into dest.
func decodeJSON(r io.Reader, dest any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// jsonMarshal is json.Marshal with a descriptive error.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
