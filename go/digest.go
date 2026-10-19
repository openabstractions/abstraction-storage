package storage

import contentdigest "github.com/openabstractions/abstraction-storage/go/digest"

// ValidDigest reports whether d is in the one digest grammar every content
// address uses (STO-D1). The digest package owns the rule.
func ValidDigest(d string) bool { return contentdigest.Valid(d) }
