package collab

import (
	"encoding/json"
	"testing"
)

func TestApplyChanges(t *testing.T) {
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
			got, _, err := applyChanges(toUnits(tt.doc), json.RawMessage(tt.changes))
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && fromUnits(got) != tt.want {
				t.Errorf("got %q, want %q", fromUnits(got), tt.want)
			}
		})
	}
}

func TestReplaceChanges(t *testing.T) {
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
			got, ok := replaceChanges(toUnits(tt.old), toUnits(tt.new))
			if !ok {
				t.Fatal("documents differ but ok is false")
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
			// The result must turn old into new.
			applied, _, err := applyChanges(toUnits(tt.old), got)
			if err != nil {
				t.Fatal(err)
			}
			if fromUnits(applied) != tt.new {
				t.Errorf("applied %q, want %q", fromUnits(applied), tt.new)
			}
		})
	}
}

func TestReplaceChangesEqualDocuments(t *testing.T) {
	if _, ok := replaceChanges(toUnits("same"), toUnits("same")); ok {
		t.Error("equal documents must produce no changes")
	}
}
