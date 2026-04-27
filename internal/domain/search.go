package domain

import "strings"

// Predicate selects nodes during Tree.Search.
type Predicate interface {
	Match(n Node) bool
}

// PredicateFunc adapts a plain function to the Predicate interface.
type PredicateFunc func(n Node) bool

// Match implements Predicate.
func (f PredicateFunc) Match(n Node) bool { return f(n) }

// MatchAll is a predicate that matches every node.
var MatchAll Predicate = PredicateFunc(func(Node) bool { return true })

// MatchNone is a predicate that matches no node.
var MatchNone Predicate = PredicateFunc(func(Node) bool { return false })

// MatchName returns a predicate that matches nodes whose name contains substr
// (case-insensitive). Both folders and connections are considered.
func MatchName(substr string) Predicate {
	needle := strings.ToLower(substr)
	return PredicateFunc(func(n Node) bool {
		switch v := n.(type) {
		case *FolderNode:
			return strings.Contains(strings.ToLower(v.Name), needle)
		case *ConnectionNode:
			return strings.Contains(strings.ToLower(v.Name), needle)
		}
		return false
	})
}

// MatchTag returns a predicate that matches nodes carrying the given tag.
// Tag comparison is case-insensitive.
func MatchTag(tag string) Predicate {
	want := strings.ToLower(tag)
	hasTag := func(tags []string) bool {
		for _, t := range tags {
			if strings.ToLower(t) == want {
				return true
			}
		}
		return false
	}
	return PredicateFunc(func(n Node) bool {
		switch v := n.(type) {
		case *FolderNode:
			return hasTag(v.Tags)
		case *ConnectionNode:
			return hasTag(v.Tags)
		}
		return false
	})
}

// MatchProtocol returns a predicate that matches connection nodes whose
// ProtocolID equals id. Folders never match.
func MatchProtocol(id string) Predicate {
	return PredicateFunc(func(n Node) bool {
		c, ok := n.(*ConnectionNode)
		return ok && c.ProtocolID == id
	})
}

// And returns a predicate that matches when every child predicate matches.
// An empty And matches every node (vacuously true).
func And(preds ...Predicate) Predicate {
	return PredicateFunc(func(n Node) bool {
		for _, p := range preds {
			if p == nil {
				continue
			}
			if !p.Match(n) {
				return false
			}
		}
		return true
	})
}

// Or returns a predicate that matches when any child predicate matches.
// An empty Or matches no node.
func Or(preds ...Predicate) Predicate {
	return PredicateFunc(func(n Node) bool {
		for _, p := range preds {
			if p != nil && p.Match(n) {
				return true
			}
		}
		return false
	})
}

// Not returns a predicate that matches when inner does not match. A nil inner
// is treated as MatchNone, so Not(nil) matches everything.
func Not(inner Predicate) Predicate {
	return PredicateFunc(func(n Node) bool {
		if inner == nil {
			return true
		}
		return !inner.Match(n)
	})
}
