package domain

import (
	"testing"
)

// mockNode implements the Node interface but is neither a FolderNode nor a ConnectionNode.
type mockNode struct {
	id ID
}

func (m mockNode) NodeID() ID         { return m.id }
func (m mockNode) NodeParent() ID     { return NilID }
func (m mockNode) NodeKind() NodeKind { return NodeKind("mock") }

func TestMatchTag(t *testing.T) {
	tests := []struct {
		name      string
		searchTag string
		node      Node
		want      bool
	}{
		{
			name:      "ConnectionNode exact match",
			searchTag: "prod",
			node:      &ConnectionNode{Tags: []string{"dev", "prod"}},
			want:      true,
		},
		{
			name:      "FolderNode exact match",
			searchTag: "dev",
			node:      &FolderNode{Tags: []string{"dev"}},
			want:      true,
		},
		{
			name:      "ConnectionNode case-insensitive match (search tag uppercase)",
			searchTag: "PROD",
			node:      &ConnectionNode{Tags: []string{"dev", "prod"}},
			want:      true,
		},
		{
			name:      "FolderNode case-insensitive match (node tag uppercase)",
			searchTag: "dev",
			node:      &FolderNode{Tags: []string{"DEV"}},
			want:      true,
		},
		{
			name:      "ConnectionNode no match",
			searchTag: "prod",
			node:      &ConnectionNode{Tags: []string{"dev", "test"}},
			want:      false,
		},
		{
			name:      "FolderNode no match",
			searchTag: "test",
			node:      &FolderNode{Tags: []string{"dev"}},
			want:      false,
		},
		{
			name:      "ConnectionNode empty tags",
			searchTag: "prod",
			node:      &ConnectionNode{Tags: []string{}},
			want:      false,
		},
		{
			name:      "Unknown node type",
			searchTag: "prod",
			node:      mockNode{id: NewID()},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := MatchTag(tt.searchTag)
			got := predicate.Match(tt.node)
			if got != tt.want {
				t.Errorf("MatchTag(%q).Match() = %v, want %v", tt.searchTag, got, tt.want)
			}
		})
	}
}

func TestAnd(t *testing.T) {
	node := mockNode{id: NewID()}

	tests := []struct {
		name  string
		preds []Predicate
		want  bool
	}{
		{
			name:  "empty And (vacuously true)",
			preds: []Predicate{},
			want:  true,
		},
		{
			name:  "single true",
			preds: []Predicate{MatchAll},
			want:  true,
		},
		{
			name:  "single false",
			preds: []Predicate{MatchNone},
			want:  false,
		},
		{
			name:  "multiple true",
			preds: []Predicate{MatchAll, MatchAll},
			want:  true,
		},
		{
			name:  "multiple with false",
			preds: []Predicate{MatchAll, MatchNone, MatchAll},
			want:  false,
		},
		{
			name:  "skip nil (true)",
			preds: []Predicate{nil, MatchAll},
			want:  true,
		},
		{
			name:  "skip nil (false)",
			preds: []Predicate{MatchNone, nil},
			want:  false,
		},
		{
			name:  "all nil",
			preds: []Predicate{nil, nil},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			predicate := And(tt.preds...)
			got := predicate.Match(node)
			if got != tt.want {
				t.Errorf("And().Match() = %v, want %v", got, tt.want)
			}
		})
	}
}
