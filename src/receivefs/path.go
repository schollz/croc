// Package receivefs validates untrusted receive paths and provides filesystem
// operations rooted at a directory handle.
package receivefs

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Kind identifies the type of object an untrusted manifest will create.
type Kind uint8

const (
	KindFile Kind = iota
	KindDirectory
	KindSymlink
)

// Entry is an untrusted destination path and its intended type.
type Entry struct {
	Path string
	Kind Kind
}

// ErrUnsafePath marks a path that is not safe to create portably.
var ErrUnsafePath = errors.New("unsafe receive path")

// ErrSensitivePath marks a path containing a component that receivers must
// never create from untrusted transfer metadata.
var ErrSensitivePath = errors.New("sensitive receive path")

// ErrPathCollision marks destinations that are ambiguous on a supported
// filesystem or conflict as file and directory paths.
var ErrPathCollision = errors.New("receive path collision")

var foldCase = cases.Fold()

// Normalize converts a portable relative path to NFC slash form. Root may be
// represented by an empty string or dot only when allowRoot is true.
func Normalize(name string, allowRoot bool) (string, error) {
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: invalid UTF-8", ErrUnsafePath)
	}
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || name == "." {
		if allowRoot {
			return ".", nil
		}
		return "", fmt.Errorf("%w: empty destination", ErrUnsafePath)
	}
	if strings.HasPrefix(name, "/") || hasWindowsVolume(name) {
		return "", fmt.Errorf("%w: absolute destination %q", ErrUnsafePath, name)
	}

	components := strings.Split(name, "/")
	normalized := make([]string, 0, len(components))
	for _, component := range components {
		if component == "" || component == "." {
			continue
		}
		if component == ".." {
			return "", fmt.Errorf("%w: parent traversal in %q", ErrUnsafePath, name)
		}
		component = norm.NFC.String(component)
		if err := validateComponent(component); err != nil {
			return "", fmt.Errorf("%w in %q: %v", ErrUnsafePath, name, err)
		}
		if ForbiddenComponent(component) {
			return "", fmt.Errorf("%w: %q in %q", ErrSensitivePath, component, name)
		}
		normalized = append(normalized, component)
	}
	if len(normalized) == 0 {
		if allowRoot {
			return ".", nil
		}
		return "", fmt.Errorf("%w: empty destination", ErrUnsafePath)
	}
	return strings.Join(normalized, "/"), nil
}

// ForbiddenComponent reports whether a normalized path component names a
// sensitive receiver-owned directory. Matching uses the same portable Unicode
// collision key as manifest duplicate detection.
func ForbiddenComponent(component string) bool {
	switch CollisionKey(component) {
	case ".ssh", ".git", ".gnupg":
		return true
	default:
		return false
	}
}

func hasWindowsVolume(name string) bool {
	if strings.HasPrefix(name, "//") {
		return true
	}
	return len(name) >= 2 && ((name[0] >= 'a' && name[0] <= 'z') ||
		(name[0] >= 'A' && name[0] <= 'Z')) && name[1] == ':'
}

func validateComponent(component string) error {
	for _, r := range component {
		if unicode.IsControl(r) || !unicode.IsGraphic(r) || !unicode.IsPrint(r) {
			return fmt.Errorf("non-printable character U+%04X", r)
		}
	}
	if strings.Contains(component, ":") {
		return errors.New("Windows alternate-data-stream syntax")
	}
	if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
		return errors.New("Windows-trimmed trailing character")
	}
	stem := component
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	upper := strings.ToUpper(stem)
	if isWindowsDeviceName(upper) {
		return errors.New("Windows reserved device name")
	}
	return nil
}

func isWindowsDeviceName(stem string) bool {
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$", "CONIN$", "CONOUT$":
		return true
	}
	if len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) {
		return stem[3] >= '1' && stem[3] <= '9'
	}
	if len([]rune(stem)) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) {
		last := []rune(stem)[3]
		return last == '¹' || last == '²' || last == '³'
	}
	return false
}

// CollisionKey returns the portable comparison form used for duplicate
// detection on case-insensitive and normalization-insensitive filesystems.
func CollisionKey(name string) string {
	return foldCase.String(norm.NFC.String(name))
}

// ValidateEntries normalizes a complete untrusted manifest and rejects exact,
// case-folded, normalization-equivalent, and file/directory collisions.
func ValidateEntries(entries []Entry) ([]Entry, error) {
	normalized := make([]Entry, len(entries))
	type destination struct {
		path string
		kind Kind
	}
	byKey := make(map[string]destination, len(entries))
	for i, entry := range entries {
		clean, err := Normalize(entry.Path, entry.Kind == KindDirectory)
		if err != nil {
			return nil, err
		}
		key := CollisionKey(clean)
		if previous, exists := byKey[key]; exists {
			return nil, fmt.Errorf("%w: %q conflicts with %q", ErrPathCollision, clean, previous.path)
		}
		byKey[key] = destination{path: clean, kind: entry.Kind}
		normalized[i] = Entry{Path: clean, Kind: entry.Kind}
	}

	for _, entry := range normalized {
		ancestor := path.Dir(entry.Path)
		for ancestor != "." && ancestor != "/" {
			if existing, ok := byKey[CollisionKey(ancestor)]; ok && existing.kind != KindDirectory {
				return nil, fmt.Errorf("%w: %q is beneath non-directory %q", ErrPathCollision, entry.Path, existing.path)
			}
			ancestor = path.Dir(ancestor)
		}
	}
	return normalized, nil
}
