package correlation

import "testing"

func TestFromHeader(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		want  []Key
	}{
		{
			name:  "request id",
			key:   "X-Request-Id",
			value: "REQ-12345",
			want:  []Key{{Type: "request_id", Value: "req-12345"}},
		},
		{
			name:  "correlation id",
			key:   "x-correlation-id",
			value: "corr-abc-789",
			want:  []Key{{Type: "correlation_id", Value: "corr-abc-789"}},
		},
		{
			name:  "traceparent",
			key:   "traceparent",
			value: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00",
			want:  []Key{{Type: "trace_id", Value: "4bf92f3577b34da6a3ce929d0e0e4736"}},
		},
		{
			name:  "cloud trace context",
			key:   "x-cloud-trace-context",
			value: "105445aa7843bc8bf206b12000100000/123;o=1",
			want:  []Key{{Type: "trace_id", Value: "105445aa7843bc8bf206b12000100000"}},
		},
		{
			name:  "unsupported header",
			key:   "content-type",
			value: "application/json",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FromHeader(tt.key, tt.value)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %d keys, got %d: %#v", len(tt.want), len(got), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("key[%d] mismatch: got %#v want %#v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFromMessage(t *testing.T) {
	msg := `error handling request_id=REQ-999 traceparent=00-4bf92f3577b34da6a3ce929d0e0e4736-1111111111111111-01 x-correlation-id:"corr-777"`
	keys := FromMessage(msg)
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d: %#v", len(keys), keys)
	}

	want := map[string]string{
		"request_id":     "req-999",
		"trace_id":       "4bf92f3577b34da6a3ce929d0e0e4736",
		"correlation_id": "corr-777",
	}
	for _, key := range keys {
		if expected, ok := want[key.Type]; !ok {
			t.Fatalf("unexpected key type: %s", key.Type)
		} else if key.Value != expected {
			t.Fatalf("unexpected %s value: got %s want %s", key.Type, key.Value, expected)
		}
	}
}

func TestFromMessageDedupes(t *testing.T) {
	msg := `request_id=req-123 request-id=req-123 x-request-id=req-123`
	keys := FromMessage(msg)
	if len(keys) != 1 {
		t.Fatalf("expected deduped single key, got %d: %#v", len(keys), keys)
	}
	if keys[0].Type != "request_id" || keys[0].Value != "req-123" {
		t.Fatalf("unexpected key: %#v", keys[0])
	}
}
