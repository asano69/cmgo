// Package ot holds the change sets of @codemirror/collab: the edits that turn
// one version of a document into the next. It knows nothing about servers or
// connections.
//
// Positions count UTF-16 code units, like CodeMirror's, so a document is a
// []uint16 (see Encode and Decode).
package ot

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf16"
)

// Encode converts text to the UTF-16 units that positions refer to.
func Encode(s string) []uint16 { return utf16.Encode([]rune(s)) }

// Decode converts UTF-16 units back to text.
func Decode(u []uint16) string { return string(utf16.Decode(u)) }

// Edit replaces the units From..To of a document with Text.
type Edit struct {
	From, To int
	Text     []uint16
}

// ChangeSet is a list of edits for a document of a known length. Its JSON form
// is that of CodeMirror's ChangeSet: a number keeps that many units, and an
// array [deleted, ...insertedLines] replaces them.
//
// A ChangeSet is immutable and always valid; make one with NewChangeSet, Diff
// or by decoding JSON. The zero value is "no changes to an empty document".
type ChangeSet struct {
	length int
	edits  []Edit
}

// NewChangeSet returns the changes that apply edits to a document of docLen
// units. The edits must be sorted, must not overlap and must lie inside the
// document. Edits that change nothing are dropped. The texts are not copied.
func NewChangeSet(docLen int, edits ...Edit) (ChangeSet, error) {
	if docLen < 0 {
		return ChangeSet{}, fmt.Errorf("%d is not a valid document length", docLen)
	}
	kept := make([]Edit, 0, len(edits))
	pos := 0
	for i, e := range edits {
		if e.From < pos || e.To < e.From || e.To > docLen {
			return ChangeSet{}, fmt.Errorf(
				"edit %d (%d..%d) is out of order or outside the document (%d units)",
				i, e.From, e.To, docLen)
		}
		pos = e.To
		if e.To > e.From || len(e.Text) > 0 {
			kept = append(kept, e)
		}
	}
	return ChangeSet{length: docLen, edits: kept}, nil
}

// Len is the length of the document that the changes apply to.
func (c ChangeSet) Len() int { return c.length }

// NewLen is the length of the document after the changes.
func (c ChangeSet) NewLen() int {
	n := c.length
	for _, e := range c.edits {
		n += len(e.Text) - (e.To - e.From)
	}
	return n
}

// Empty reports whether the changes leave the document as it is.
func (c ChangeSet) Empty() bool { return len(c.edits) == 0 }

// Edits returns the edits in document order. They must not be modified.
func (c ChangeSet) Edits() []Edit { return c.edits }

// Apply returns doc with the changes applied. doc is not modified.
func (c ChangeSet) Apply(doc []uint16) ([]uint16, error) {
	if len(doc) != c.length {
		return nil, fmt.Errorf("changes cover %d units but the document has %d", c.length, len(doc))
	}
	out := make([]uint16, 0, c.NewLen())
	pos := 0
	for _, e := range c.edits {
		out = append(out, doc[pos:e.From]...)
		out = append(out, e.Text...)
		pos = e.To
	}
	return append(out, doc[pos:]...), nil
}

// MarshalJSON implements json.Marshaler.
func (c ChangeSet) MarshalJSON() ([]byte, error) {
	sections := make([]any, 0, 2*len(c.edits)+1)
	pos := 0
	for _, e := range c.edits {
		if e.From > pos {
			sections = append(sections, e.From-pos)
		}
		section := []any{e.To - e.From}
		if len(e.Text) > 0 {
			for _, line := range strings.Split(Decode(e.Text), "\n") {
				section = append(section, line)
			}
		}
		sections = append(sections, section)
		pos = e.To
	}
	if c.length > pos {
		sections = append(sections, c.length-pos)
	}
	return json.Marshal(sections)
}

// UnmarshalJSON implements json.Unmarshaler.
func (c *ChangeSet) UnmarshalJSON(data []byte) error {
	var sections []any
	if err := json.Unmarshal(data, &sections); err != nil {
		return fmt.Errorf("invalid changes: %w", err)
	}

	var edits []Edit
	pos := 0
	for _, section := range sections {
		switch v := section.(type) {
		case float64:
			n, err := unitCount(v)
			if err != nil {
				return err
			}
			pos += n
		case []any:
			deleted, inserted, err := replacement(v)
			if err != nil {
				return err
			}
			edits = append(edits, Edit{From: pos, To: pos + deleted, Text: inserted})
			pos += deleted
		default:
			return fmt.Errorf("invalid section %v", section)
		}
	}
	set, err := NewChangeSet(pos, edits...)
	if err != nil {
		return err
	}
	*c = set
	return nil
}

// unitCount parses a non-negative whole number from decoded JSON.
func unitCount(v any) (int, error) {
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
	if deleted, err = unitCount(section[0]); err != nil {
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
	return deleted, Encode(strings.Join(lines, "\n")), nil
}

func isHighSurrogate(u uint16) bool { return u >= 0xD800 && u < 0xDC00 }

func isLowSurrogate(u uint16) bool { return u >= 0xDC00 && u < 0xE000 }

// Diff returns the changes that turn oldDoc into newDoc. They replace only the
// range that differs, so clients keep their cursors in the unchanged text. The
// result is Empty when the documents are equal.
func Diff(oldDoc, newDoc []uint16) ChangeSet {
	prefix := 0
	for prefix < len(oldDoc) && prefix < len(newDoc) && oldDoc[prefix] == newDoc[prefix] {
		prefix++
	}
	if prefix == len(oldDoc) && prefix == len(newDoc) {
		return ChangeSet{length: len(oldDoc)}
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

	edit := Edit{
		From: prefix,
		To:   len(oldDoc) - suffix,
		Text: slices.Clone(newDoc[prefix : len(newDoc)-suffix]),
	}
	return ChangeSet{length: len(oldDoc), edits: []Edit{edit}}
}
