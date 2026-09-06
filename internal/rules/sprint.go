package rules

import "fmt"

// sprint is fmt.Sprint behind a tiny seam so Norm's fallback is explicit.
func sprint(v any) string { return fmt.Sprint(v) }
