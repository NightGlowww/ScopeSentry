package cip

import (
	"testing"

	"github.com/Autumn-27/ScopeSentry/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestBuildListMatch(t *testing.T) {
	m := buildListMatch(models.CIPListRequest{Project: "p", Company: "Acme", Keyword: "a.*b"})
	and, ok := m["$and"].(bson.A)
	if !ok || len(and) != 4 {
		t.Fatalf("conds = %v, want 4 ($and: company exists/project/company/keyword)", m)
	}

	empty := buildListMatch(models.CIPListRequest{})
	if _, ok := empty["company"]; !ok {
		t.Fatalf("default match = %v, want company condition", empty)
	}

	kw := keywordMatch("a.*b")
	or, ok := kw["$or"].(bson.A)
	if !ok || len(or) != 3 {
		t.Fatalf("keyword = %v", kw)
	}
	re := or[0].(bson.M)["company"].(primitive.Regex)
	if re.Pattern != "a\\.\\*b" || re.Options != "i" {
		t.Fatalf("regex = %q/%q", re.Pattern, re.Options)
	}
}
