package cmgo

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf16"
)

// CodeMirror counts document positions in UTF-16 code units, so the server
// keeps the document in that form and applies change sets the way clients do.

func toUnits(s string) []uint16 { return utf16.Encode([]rune(s)) }

func fromUnits(u []uint16) string { return string(utf16.Decode(u)) }

// length parses a non-negative whole number from decoded JSON.
func length(v any) (int, error) {
	f, ok := v.(float64)
	if !ok || f < 0 || f != math.Trunc(f) || f > math.MaxInt32 {
		return 0, fmt.Errorf("%v is not a valid length", v)
	}
	return int(f), nil
}

// replacement parses a [deleted, ...insertedLines] section.
func replacement(section []any) (deleted int, inserted []uint16, err error) {
	if len(section) == 0 {
		return 0, nil, fmt.Errorf("replace section is empty")
	}
	if deleted, err = length(section[0]); err != nil {
		return 0, nil, err
	}
	lines := make([]string, 0, len(section)-1)
	for _, v := range section[1:] {
		line, ok := v.(string)
		if !ok {
			return 0, nil, fmt.Errorf("inserted line %v is not a string", v)
		}
		lines = append(lines, line)
	}
	return deleted, toUnits(strings.Join(lines, "\n")), nil
}

// parseChanges turns a CodeMirror ChangeSet (its JSON form) into edits on a
// document of docLen units.
//
// A section is either a number (keep that many units) or an array
// [deleted, ...insertedLines].
func parseChanges(docLen int, changes json.RawMessage) ([]Edit, error) {
	var sections []any
	if err := json.Unmarshal(changes, &sections); err != nil {
		return nil, fmt.Errorf("invalid changes: %w", err)
	}

	var edits []Edit
	pos := 0
	for _, section := range sections {
		switch v := section.(type) {
		case float64:
			n, err := length(v)
			if err != nil {
				return nil, err
			}
			pos += n
		case []any:
			deleted, inserted, err := replacement(v)
			if err != nil {
				return nil, err
			}
			if deleted > 0 || len(inserted) > 0 {
				edits = append(edits, Edit{From: pos, To: pos + deleted, Text: inserted})
			}
			pos += deleted
		default:
			return nil, fmt.Errorf("invalid section %v", section)
		}
		if pos > docLen {
			return nil, fmt.Errorf("changes are longer than the document (%d units)", docLen)
		}
	}
	if pos != docLen {
		return nil, fmt.Errorf("changes cover %d units but the document has %d", pos, docLen)
	}
	return edits, nil
}

// applyEdits returns doc with the edits applied. doc is not modified.
func applyEdits(doc []uint16, edits []Edit) []uint16 {
	out := make([]uint16, 0, len(doc))
	pos := 0
	for _, e := range edits {
		out = append(out, doc[pos:e.From]...)
		out = append(out, e.Text...)
		pos = e.To
	}
	return append(out, doc[pos:]...)
}

// applyChanges applies a CodeMirror ChangeSet (its JSON form) to doc and
// returns the new document and the edits it consists of. doc is not modified.
func applyChanges(doc []uint16, changes json.RawMessage) ([]uint16, []Edit, error) {
	edits, err := parseChanges(len(doc), changes)
	if err != nil {
		return nil, nil, err
	}
	return applyEdits(doc, edits), edits, nil
}

// applyUpdates applies updates in order to the document and returns the new
// document with one Change per update. Nothing is applied if one fails.
func applyUpdates(doc []uint16, updates []Update) ([]uint16, []Change, error) {
	changes := make([]Change, 0, len(updates))
	for i, u := range updates {
		next, edits, err := applyChanges(doc, u.Changes)
		if err != nil {
			return nil, nil, fmt.Errorf("update %d: %w", i, err)
		}
		changes = append(changes, Change{Before: doc, Edits: edits})
		doc = next
	}
	return doc, changes, nil
}

func isHighSurrogate(u uint16) bool { return u >= 0xD800 && u < 0xDC00 }

func isLowSurrogate(u uint16) bool { return u >= 0xDC00 && u < 0xE000 }

// replaceChanges returns the ChangeSet JSON that turns oldDoc into newDoc. It
// replaces only the range that differs, so clients keep their cursors in the
// unchanged text. ok is false when the documents are equal.
func replaceChanges(oldDoc, newDoc []uint16) (changes json.RawMessage, ok bool) {
	prefix := 0
	for prefix < len(oldDoc) && prefix < len(newDoc) && oldDoc[prefix] == newDoc[prefix] {
		prefix++
	}
	if prefix == len(oldDoc) && prefix == len(newDoc) {
		return nil, false
	}
	// Never split a surrogate pair: the inserted text must stay valid.
	if prefix > 0 && isHighSurrogate(oldDoc[prefix-1]) {
		prefix--
	}

	suffix := 0
	for suffix < len(oldDoc)-prefix && suffix < len(newDoc)-prefix &&
		oldDoc[len(oldDoc)-1-suffix] == newDoc[len(newDoc)-1-suffix] {
		suffix++
	}
	if suffix > 0 && isLowSurrogate(oldDoc[len(oldDoc)-suffix]) {
		suffix--
	}

	var sections []any
	if prefix > 0 {
		sections = append(sections, prefix)
	}
	section := []any{len(oldDoc) - prefix - suffix}
	if inserted := newDoc[prefix : len(newDoc)-suffix]; len(inserted) > 0 {
		for _, line := range strings.Split(fromUnits(inserted), "\n") {
			section = append(section, line)
		}
	}
	sections = append(sections, section)
	if suffix > 0 {
		sections = append(sections, suffix)
	}

	data, err := json.Marshal(sections)
	if err != nil {
		// Unreachable: sections only hold numbers and strings.
		return nil, false
	}
	return data, true
}
