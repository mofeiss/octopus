package relay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// [fork] applyParamOverrideToHTTPRequest patches the final outbound JSON body
// with the channel-level param_override after protocol transformation.
func applyParamOverrideToHTTPRequest(req *http.Request, paramOverride *string) error {
	if paramOverride == nil || strings.TrimSpace(*paramOverride) == "" {
		return nil
	}
	if req == nil {
		return fmt.Errorf("request is nil")
	}

	override := make(map[string]any)
	if err := json.Unmarshal([]byte(strings.TrimSpace(*paramOverride)), &override); err != nil {
		return fmt.Errorf("failed to decode param_override: %w", err)
	}
	if override == nil {
		return fmt.Errorf("param_override must be a JSON object")
	}

	body, err := readAndRestoreHTTPRequestBody(req)
	if err != nil {
		return fmt.Errorf("failed to read outbound request body: %w", err)
	}

	base := make(map[string]any)
	if strings.TrimSpace(string(body)) != "" {
		if err := json.Unmarshal(body, &base); err != nil {
			return fmt.Errorf("failed to decode outbound request body: %w", err)
		}
		if base == nil {
			return fmt.Errorf("outbound request body must be a JSON object")
		}
	}

	mergeJSONObjects(base, override)

	patchedBody, err := json.Marshal(base)
	if err != nil {
		return fmt.Errorf("failed to encode patched outbound request body: %w", err)
	}
	setHTTPRequestBody(req, patchedBody)
	return nil
}

func mergeJSONObjects(base map[string]any, override map[string]any) {
	for key, overrideValue := range override {
		baseObject, baseIsObject := base[key].(map[string]any)
		overrideObject, overrideIsObject := overrideValue.(map[string]any)
		if baseIsObject && overrideIsObject {
			mergeJSONObjects(baseObject, overrideObject)
			continue
		}
		base[key] = overrideValue
	}
}

func readAndRestoreHTTPRequestBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		defer body.Close()
		return io.ReadAll(body)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func setHTTPRequestBody(req *http.Request, body []byte) {
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
}
