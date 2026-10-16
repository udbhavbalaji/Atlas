package interpret

import "time"

// ResolveTime applies the existing English time grammar to a spoken field
// answer. Unresolved or ambiguous phrases remain questions for the caller.
func ResolveTime(text, zone, referenceAt, field string) (string, string, bool) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return "", "", false
	}
	reference, err := time.Parse(time.RFC3339Nano, referenceAt)
	if err != nil {
		return "", "", false
	}
	if instant, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return instant.UTC().Format(time.RFC3339Nano), "", true
	}
	instant, repeat, _, questions := parseTime(text, reference.In(loc), loc, field)
	return instant, repeat, instant != "" && len(questions) == 0
}
