package domain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeRepositoryTagsRetainsUnknownEmptyAndChinese(t *testing.T) {
	unknown, err := NormalizeRepositoryTags(nil)
	if err != nil || unknown != nil {
		t.Fatal("unknown tags must remain nil")
	}
	empty, err := NormalizeRepositoryTags([]string{})
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("known empty tags must remain non-nil")
	}
	input := []string{" RAG ", "rag", " 中文写作\n", "中文写作", " Mixed Case ", ""}
	tags, err := NormalizeRepositoryTags(input)
	if err != nil || !reflect.DeepEqual(tags, []string{"rag", "中文写作", "mixed case"}) || input[0] != " RAG " {
		t.Fatalf("tags=%v err=%v", tags, err)
	}
	for _, invalid := range [][]string{{"embedded\nline"}, {"delete\x7f"}, {string([]byte{0xff})}, {strings.Repeat("中", 81)}} {
		if _, err := NormalizeRepositoryTags(invalid); err == nil {
			t.Fatalf("invalid tag accepted: %q", invalid)
		}
	}
	many := make([]string, 65)
	for i := range many {
		many[i] = fmt.Sprintf("tag-%d", i)
	}
	if _, err := NormalizeRepositoryTags(many); err == nil {
		t.Fatal("65 unique tags accepted")
	}
}
