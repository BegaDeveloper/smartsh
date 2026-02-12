package ai

import (
	"errors"
	"strings"
)

const maxDebugRawResponseLength = 2000

type StrictJSONResponseError struct {
	Cause       error
	RawResponse string
}

func (strictJSONResponseError *StrictJSONResponseError) Error() string {
	if strictJSONResponseError == nil || strictJSONResponseError.Cause == nil {
		return "strict JSON parse failure"
	}
	return strictJSONResponseError.Cause.Error()
}

func (strictJSONResponseError *StrictJSONResponseError) Unwrap() error {
	if strictJSONResponseError == nil {
		return nil
	}
	return strictJSONResponseError.Cause
}

func DebugRawResponseFromError(err error) (string, bool) {
	var strictJSONResponseError *StrictJSONResponseError
	if !errors.As(err, &strictJSONResponseError) {
		return "", false
	}
	return sanitizeDebugRawResponse(strictJSONResponseError.RawResponse), true
}

func sanitizeDebugRawResponse(raw string) string {
	sanitized := strings.TrimSpace(raw)
	sanitized = strings.ReplaceAll(sanitized, "\r", "\\r")
	sanitized = strings.ReplaceAll(sanitized, "\n", "\\n")
	sanitized = strings.ReplaceAll(sanitized, "\t", "\\t")
	sanitized = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, sanitized)

	if len(sanitized) > maxDebugRawResponseLength {
		return sanitized[:maxDebugRawResponseLength] + "...<truncated>"
	}
	if sanitized == "" {
		return "<empty>"
	}
	return sanitized
}
