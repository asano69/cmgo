package ot

import (
	"encoding/json"
	"testing"
)

func TestApplyJSON(t *testing.T) {
	tests := []struct {
		name    string
		doc     string
		changes string
		want    string
		wantErr bool
	}{
		{"insert into empty", "", `[[0,"abc"]]`, "abc", false},
		{"replace in the middle", "hello", `[2,[1,"X"],2]`, "heXlo", false},
		{"insert lines", "", `[[0,"a","b"]]`, "a\nb", false},
		{"delete", "hello", `[1,[3],1]`, "ho", false},
		{"emoji counts as two units", "😀a", `[2,[1,"b"]]`, "😀b", false},
		{"too short", "abc", `[2]`, "", true},
		{"too long", "abc", `[5]`, "", true},
		{"negative length", "abc", `[-1]`, "", true},
		{"fractional length", "abc", `[1.5]`, "", true},
		{"unknown section", "abc", `["x"]`, "", true},
		{"empty replace section", "abc", `[[]]`, "", true},
		{"not an array", "abc", `{}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cs ChangeSet
			err := json.Unmarshal([]byte(tt.changes), &cs)
			var got []uint16
			if err == nil {
				got, err = cs.Apply(Encode(tt.doc))
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && Decode(got) != tt.want {
				t.Errorf("got %q, want %q", Decode(got), tt.want)
			}
		})
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	for _, in := range []string{`[]`, `[3]`, `[2,[1,"X"],2]`, `[[0,"a","b"]]`, `[1,[3],1]`} {
		var cs ChangeSet
		if err := json.Unmarshal([]byte(in), &cs); err != nil {
			t.Fatal(err)
		}
		out, err := json.Marshal(cs)
		if err != nil {
			t.Fatal(err)
		}
		if string(out) != in {
			t.Errorf("got %s, want %s", out, in)
		}
	}
}

func TestLenAndNewLen(t *testing.T) {
	cs, err := NewChangeSet(5, Edit{From: 1, To: 3, Text: Encode("xyz")})
	if err != nil {
		t.Fatal(err)
	}
	if cs.Len() != 5 || cs.NewLen() != 6 {
		t.Errorf("Len = %d, NewLen = %d; want 5, 6", cs.Len(), cs.NewLen())
	}
}

func TestNewChangeSetRejectsBadEdits(t *testing.T) {
	tests := []struct {
		name  string
		edits []Edit
	}{
		{"past the end", []Edit{{From: 2, To: 5}}},
		{"backwards", []Edit{{From: 2, To: 1}}},
		{"overlapping", []Edit{{From: 0, To: 2}, {From: 1, To: 3}}},
		{"unsorted", []Edit{{From: 2, To: 3}, {From: 0, To: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewChangeSet(4, tt.edits...); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name, old, new, want string
	}{
		{"insert in the middle", "abcd", "abXcd", `[2,[0,"X"],2]`},
		{"delete in the middle", "abXcd", "abcd", `[2,[1],2]`},
		{"append lines", "ab", "abc\nd", `[2,[0,"c","d"]]`},
		{"replace all", "ab", "cd", `[[2,"cd"]]`},
		{"emoji is not split", "😀", "😁", `[[2,"😁"]]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := Diff(Encode(tt.old), Encode(tt.new))
			got, err := json.Marshal(cs)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
			// The result must turn old into new.
			applied, err := cs.Apply(Encode(tt.old))
			if err != nil {
				t.Fatal(err)
			}
			if Decode(applied) != tt.new {
				t.Errorf("applied %q, want %q", Decode(applied), tt.new)
			}
		})
	}
}

func TestDiffEqualDocuments(t *testing.T) {
	if !Diff(Encode("same"), Encode("same")).Empty() {
		t.Error("equal documents must produce no changes")
	}
}
