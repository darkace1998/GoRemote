package main

import (
	"fmt"
	"testing"
	"github.com/darkace1998/GoRemote/internal/domain"
)

func BenchmarkFindByConnection(b *testing.B) {
	r := &sessionRegistry{
		items: make(map[domain.ID]*sessionTab),
	}

	// Create a large number of sessions
	const numSessions = 1000
	var targetConnID string

	for i := 0; i < numSessions; i++ {
		hid := domain.NewID()
		connID := fmt.Sprintf("conn-%d", i)
		r.items[hid] = &sessionTab{
			hid:    hid,
			connID: connID,
		}

		if i == numSessions/2 {
			targetConnID = connID
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.findByConnection(targetConnID)
	}
}

func BenchmarkFindByConnection_Miss(b *testing.B) {
	r := &sessionRegistry{
		items: make(map[domain.ID]*sessionTab),
	}

	// Create a large number of sessions
	const numSessions = 1000

	for i := 0; i < numSessions; i++ {
		hid := domain.NewID()
		connID := fmt.Sprintf("conn-%d", i)
		r.items[hid] = &sessionTab{
			hid:    hid,
			connID: connID,
		}
	}

	targetConnID := "missing-conn"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.findByConnection(targetConnID)
	}
}
