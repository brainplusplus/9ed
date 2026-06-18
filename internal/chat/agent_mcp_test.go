package chat

import (
	"sync"
	"testing"

	"github.com/brainplusplus/9ed/internal/chat/acp"
)

func TestActiveMCPServersAreSnapshottedSafely(t *testing.T) {
	// Reset to a known empty state at the end so we don't pollute other tests.
	t.Cleanup(func() {
		SetActiveTerminalMCPServers(nil)
		SetActiveBrowserMCPServers(nil)
	})

	terminal := []acp.MCPServer{{Name: "terminal", Command: "t"}}
	browser := []acp.MCPServer{{Name: "browser", Command: "b"}}
	SetActiveTerminalMCPServers(terminal)
	SetActiveBrowserMCPServers(browser)

	got := activeMCPServersForOptions(SessionOptions{UseActiveTerminal: true})
	if len(got) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(got))
	}

	// Mutating the input slice after the call must not affect the snapshot.
	terminal[0].Command = "MUTATED"
	got = activeMCPServersForOptions(SessionOptions{UseActiveTerminal: true})
	for _, s := range got {
		if s.Command == "MUTATED" {
			t.Errorf("snapshot leaked external mutation: %+v", s)
		}
	}
}

// TestActiveMCPServersConcurrent stresses the lock by interleaving
// readers and writers. With CGO disabled we can't run -race, so we rely on
// repeated runs and the structural correctness of the lock.
func TestActiveMCPServersConcurrent(t *testing.T) {
	t.Cleanup(func() {
		SetActiveTerminalMCPServers(nil)
		SetActiveBrowserMCPServers(nil)
	})

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			SetActiveTerminalMCPServers([]acp.MCPServer{{Name: "t", Command: "t"}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			SetActiveBrowserMCPServers([]acp.MCPServer{{Name: "b", Command: "b"}})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			servers := activeMCPServersForOptions(SessionOptions{UseActiveTerminal: true})
			// Touch each entry so a torn read would surface in normal use.
			for j := range servers {
				_ = servers[j].Name
			}
		}
	}()
	wg.Wait()
}
