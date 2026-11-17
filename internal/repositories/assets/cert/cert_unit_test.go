package cert

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestComposeAndEmpty(t *testing.T) {
	if got := composeAnd(bson.A{}); len(got) != 0 {
		t.Fatalf("empty conds = %v, want match-all", got)
	}
	one := composeAnd(bson.A{bson.M{"project": "p"}})
	if one["project"] != "p" {
		t.Fatalf("single cond = %v", one)
	}
	two := composeAnd(bson.A{bson.M{"project": "p"}, bson.M{"host": "h"}})
	if _, ok := two["$and"]; !ok {
		t.Fatalf("two conds = %v, want $and", two)
	}
}

func TestKeywordMatchEscapesRegex(t *testing.T) {
	m := keywordMatch("a.*b")
	or, ok := m["$or"].(bson.A)
	if !ok || len(or) == 0 {
		t.Fatalf("keyword match = %v", m)
	}
	sub := or[0].(bson.M)
	re, ok := sub["subject"].(primitive.Regex)
	if !ok {
		t.Fatalf("subject matcher = %T", sub["subject"])
	}
	if re.Pattern != "a\\.\\*b" || re.Options != "i" {
		t.Fatalf("regex = %q/%q", re.Pattern, re.Options)
	}
}

func TestDetailIdentityCond(t *testing.T) {
	if cond := detailIdentityCond("sub:CN=x|CN=y|..."); cond != nil {
		t.Fatalf("sub identity should not prefilter, got %v", cond)
	}

	fp := detailIdentityCond("fp:abcdef")
	if fp == nil {
		t.Fatal("fp identity returned nil")
	}
	or, ok := fp["$or"].(bson.A)
	if !ok || len(or) == 0 {
		t.Fatalf("fp cond = %v", fp)
	}
	found := false
	for _, item := range or {
		if item.(bson.M)["tls.certificateresponse.fingerprinthash.sha256"] == "abcdef" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fp cond missing sha256 path: %v", fp)
	}

	sn := detailIdentityCond("sn:CN=CA|01AB")
	if sn == nil {
		t.Fatal("sn identity returned nil")
	}
	and, ok := sn["$and"].(bson.A)
	if !ok || len(and) != 2 {
		t.Fatalf("sn cond = %v", sn)
	}
}
