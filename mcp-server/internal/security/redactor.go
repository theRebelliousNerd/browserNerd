package security

import (
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"strings"
)

const Redacted = "[REDACTED]"

var (
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	secretPattern = regexp.MustCompile(`(?i)(password|passwd|authorization|cookie|api[_-]?key|access[_-]?token|refresh[_-]?token|client[_-]?secret)(\s*[:=]\s*)([^&\s,;"'}]+)`)
)

// Redactor removes credentials and other sensitive values before persistence.
// It is intentionally conservative: values are preserved unless their field
// name, input metadata, URL parameter, or textual shape marks them as secret.
type Redactor struct {
	sensitiveKeys map[string]struct{}
}

// NewRedactor returns the default credential redactor.
func NewRedactor(extraSensitiveKeys []string) *Redactor {
	keys := []string{
		"authorization", "proxy-authorization", "cookie", "set-cookie",
		"x-api-key", "api-key", "apikey", "password", "passwd", "passphrase",
		"secret", "client-secret", "client_secret", "access-token", "access_token",
		"refresh-token", "refresh_token", "id-token", "id_token", "token",
		"private-key", "private_key", "card-number", "card_number", "cvv", "cvc",
	}
	sensitive := make(map[string]struct{}, len(keys)+len(extraSensitiveKeys))
	for _, key := range append(keys, extraSensitiveKeys...) {
		sensitive[normalizeKey(key)] = struct{}{}
	}
	return &Redactor{sensitiveKeys: sensitive}
}

// Sanitize recursively copies a value while replacing sensitive leaves.
func (r *Redactor) Sanitize(value interface{}) interface{} {
	if r == nil {
		return value
	}
	return r.sanitizeReflect(reflect.ValueOf(value), "")
}

// SanitizeString redacts credentials embedded in URLs, authorization values,
// JSON-like log text, and JWT-shaped tokens.
func (r *Redactor) SanitizeString(value string) string {
	if r == nil {
		return value
	}
	value = RedactURL(value, r.sensitiveKeys)
	value = bearerPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := strings.Fields(match)
		if len(parts) == 0 {
			return Redacted
		}
		return parts[0] + " " + Redacted
	})
	value = jwtPattern.ReplaceAllString(value, Redacted)
	value = secretPattern.ReplaceAllString(value, `${1}${2}`+Redacted)
	return value
}

// RedactHeader returns a safe representation of an HTTP header.
func (r *Redactor) RedactHeader(name, value string) string {
	if r == nil {
		return value
	}
	if r.IsSensitiveKey(name) {
		if strings.EqualFold(strings.TrimSpace(name), "authorization") ||
			strings.EqualFold(strings.TrimSpace(name), "proxy-authorization") {
			fields := strings.Fields(value)
			if len(fields) > 1 {
				return fields[0] + " " + Redacted
			}
		}
		return Redacted
	}
	return r.SanitizeString(value)
}

// RedactInputValue returns a safe value for a browser input. The descriptor
// should combine available type, name, id, autocomplete, label, and ref data.
func (r *Redactor) RedactInputValue(descriptor, value string) string {
	if r == nil {
		r = NewRedactor(nil)
	}
	if r.IsSensitiveKey(descriptor) || isSensitiveInputDescriptor(descriptor) {
		return Redacted
	}
	return r.SanitizeString(value)
}

// IsSensitiveKey reports whether a field or header name carries secrets.
func (r *Redactor) IsSensitiveKey(key string) bool {
	if r == nil {
		return false
	}
	normalized := normalizeKey(key)
	if _, ok := r.sensitiveKeys[normalized]; ok {
		return true
	}
	for sensitive := range r.sensitiveKeys {
		if len(sensitive) >= 5 && strings.Contains(normalized, sensitive) {
			return true
		}
	}
	return false
}

func (r *Redactor) sanitizeReflect(value reflect.Value, fieldName string) interface{} {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return r.sanitizeReflect(value.Elem(), fieldName)
	}
	if r.IsSensitiveKey(fieldName) {
		return Redacted
	}

	switch value.Kind() {
	case reflect.String:
		return r.SanitizeString(value.String())
	case reflect.Map:
		out := make(map[string]interface{}, value.Len())
		sensitiveInput := r.mapDescribesSensitiveInput(value)
		iter := value.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			if sensitiveInput && isInputValueKey(key) {
				out[key] = Redacted
				continue
			}
			out[key] = r.sanitizeReflect(iter.Value(), key)
		}
		return out
	case reflect.Slice, reflect.Array:
		out := make([]interface{}, value.Len())
		for i := 0; i < value.Len(); i++ {
			out[i] = r.sanitizeReflect(value.Index(i), fieldName)
		}
		return out
	case reflect.Struct:
		out := make(map[string]interface{}, value.NumField())
		typ := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			name := field.Name
			if tag := field.Tag.Get("json"); tag != "" {
				if tagged := strings.Split(tag, ",")[0]; tagged != "" && tagged != "-" {
					name = tagged
				}
			}
			out[name] = r.sanitizeReflect(value.Field(i), name)
		}
		return out
	default:
		return value.Interface()
	}
}

func (r *Redactor) mapDescribesSensitiveInput(value reflect.Value) bool {
	iter := value.MapRange()
	for iter.Next() {
		key := normalizeKey(fmt.Sprint(iter.Key().Interface()))
		if key != "type" && key != "name" && key != "id" && key != "selector" &&
			key != "autocomplete" && key != "label" {
			continue
		}
		raw := iter.Value()
		for raw.IsValid() && (raw.Kind() == reflect.Interface || raw.Kind() == reflect.Pointer) {
			if raw.IsNil() {
				break
			}
			raw = raw.Elem()
		}
		if raw.IsValid() && raw.Kind() == reflect.String && isSensitiveInputDescriptor(raw.String()) {
			return true
		}
	}
	return false
}

func isSensitiveInputDescriptor(value string) bool {
	normalized := normalizeKey(value)
	needles := []string{
		"password", "passwd", "current-password", "new-password", "one-time-code",
		"credit-card", "cc-number", "cc-csc", "card-number", "cvv", "cvc",
		"api-key", "token", "secret",
	}
	for _, needle := range needles {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func isInputValueKey(key string) bool {
	switch normalizeKey(key) {
	case "value", "text", "content", "input":
		return true
	default:
		return false
	}
}

func normalizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = strings.ReplaceAll(key, "_", "-")
	key = strings.ReplaceAll(key, " ", "-")
	return key
}

// RedactURL removes sensitive query parameter values from a URL or URL-shaped
// string. Invalid URLs fall back to conservative textual redaction.
func RedactURL(raw string, sensitiveKeys map[string]struct{}) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.RawQuery == "" {
		return raw
	}
	query := parsed.Query()
	changed := false
	for key, values := range query {
		normalized := normalizeKey(key)
		sensitive := false
		if _, ok := sensitiveKeys[normalized]; ok {
			sensitive = true
		} else {
			for candidate := range sensitiveKeys {
				if len(candidate) >= 5 && strings.Contains(normalized, candidate) {
					sensitive = true
					break
				}
			}
		}
		if !sensitive {
			continue
		}
		for i := range values {
			values[i] = Redacted
		}
		query[key] = values
		changed = true
	}
	if !changed {
		return raw
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
