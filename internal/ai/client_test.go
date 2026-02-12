package ai

import (
	"errors"
	"strings"
	"testing"
)

func TestParseStrictResponseJSON_ValidPayload(t *testing.T) {
	t.Parallel()

	response, parseError := parseStrictResponseJSON(`{"intent":"run tests","command":"go test ./...","confidence":0.92,"risk":"low"}`)
	if parseError != nil {
		t.Fatalf("expected valid payload, got error: %v", parseError)
	}

	if response.Intent != "run tests" {
		t.Fatalf("unexpected intent: %q", response.Intent)
	}
	if response.Command != "go test ./..." {
		t.Fatalf("unexpected command: %q", response.Command)
	}
	if response.Confidence != 0.92 {
		t.Fatalf("unexpected confidence: %v", response.Confidence)
	}
	if response.Risk != "low" {
		t.Fatalf("unexpected risk: %q", response.Risk)
	}
}

func TestParseStrictResponseJSON_InvalidCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		payload string
	}{
		{name: "non json", payload: "run tests"},
		{name: "unknown field", payload: `{"intent":"run tests","command":"go test ./...","confidence":0.9,"risk":"low","extra":"x"}`},
		{name: "invalid confidence", payload: `{"intent":"run tests","command":"go test ./...","confidence":1.5,"risk":"low"}`},
		{name: "invalid risk", payload: `{"intent":"run tests","command":"go test ./...","confidence":0.9,"risk":"critical"}`},
		{name: "missing command", payload: `{"intent":"run tests","command":"","confidence":0.9,"risk":"low"}`},
		{name: "trailing content", payload: `{"intent":"run tests","command":"go test ./...","confidence":0.9,"risk":"low"} trailing`},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if _, parseError := parseStrictResponseJSON(testCase.payload); parseError == nil {
				t.Fatalf("expected parse error for case %q", testCase.name)
			}
		})
	}
}

func TestDebugRawResponseFromError(t *testing.T) {
	t.Parallel()

	originalError := errors.New("model response is not strict JSON object")
	parseError := &StrictJSONResponseError{
		Cause:       originalError,
		RawResponse: "```json\n{\"intent\":\"build\"}\n```",
	}

	raw, ok := DebugRawResponseFromError(parseError)
	if !ok {
		t.Fatalf("expected debug raw response to be available")
	}
	if !strings.Contains(raw, "```json") {
		t.Fatalf("expected sanitized raw output to include source content, got %q", raw)
	}
	if strings.Contains(raw, "\n") {
		t.Fatalf("expected newlines to be escaped in sanitized output, got %q", raw)
	}

	if _, ok := DebugRawResponseFromError(errors.New("other error")); ok {
		t.Fatalf("did not expect debug response for non-parse errors")
	}
}
