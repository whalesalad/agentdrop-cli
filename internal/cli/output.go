package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"unicode"

	"github.com/whalesalad/agentdrop-cli/internal/api"
)

// writeLine writes text plus newline to w and maps a broken pipe to a clean exit.
func writeLine(w io.Writer, text string) error {
	_, err := io.WriteString(w, text+"\n")
	return err
}

// writeJSON emits one compact JSON object followed by a newline.
func writeJSON(w io.Writer, v any) error {
	var data []byte
	switch t := v.(type) {
	case json.RawMessage:
		var buf bytes.Buffer
		if err := json.Compact(&buf, t); err != nil {
			return api.Errorf("api", "AgentDrop returned an invalid response.")
		}
		data = buf.Bytes()
	default:
		var err error
		data, err = json.Marshal(v)
		if err != nil {
			return api.Errorf("usage", "Could not encode output.")
		}
	}
	_, err := w.Write(append(data, '\n'))
	return err
}

func writePretty(w io.Writer, raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return api.Errorf("api", "AgentDrop returned an invalid response.")
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
}

// field extracts a nested member from a raw JSON object, preserving unknown
// properties inside it.
func field(raw json.RawMessage, keys ...string) (json.RawMessage, bool) {
	current := raw
	for _, key := range keys {
		var object map[string]json.RawMessage
		if json.Unmarshal(current, &object) != nil {
			return nil, false
		}
		next, ok := object[key]
		if !ok || bytes.Equal(next, []byte("null")) {
			return nil, false
		}
		current = next
	}
	return current, true
}

func stringField(raw json.RawMessage, keys ...string) string {
	v, ok := field(raw, keys...)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(v, &s) != nil {
		return ""
	}
	return s
}

// sanitize removes terminal control characters from human-facing metadata.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) || r == 0x2028 || r == 0x2029 || r == 0xfeff {
			return '�'
		}
		return r
	}, s)
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "pipe is being closed")
}

func errorJSON(err *api.Error) string {
	inner := map[string]any{"code": err.Code, "message": err.Message}
	if err.HTTPStatus != 0 {
		inner["httpStatus"] = err.HTTPStatus
	}
	if err.APICode != "" {
		inner["apiCode"] = err.APICode
	}
	if err.FileID != "" {
		inner["fileId"] = err.FileID
	}
	data, _ := json.Marshal(map[string]any{"error": inner})
	return string(data)
}

func humanSize(n int64) string { return fmt.Sprintf("%d bytes", n) }
