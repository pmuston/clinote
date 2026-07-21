package server

import (
	"context"
	"fmt"

	"github.com/pmuston/clinote/internal/notebook"
)

// batchState tracks a run-all / run-from-here in progress.
//
// Only one batch runs at a time, guarded by the same mutex as activeIdx.
type batchState struct {
	active bool
	// total and completed drive the progress label.
	total     int
	completed int
	// abort is set by Interrupt (or a failing cell) to stop before the next
	// cell starts.
	abort bool
	// failed records the 1-based position of the cell that ended the batch,
	// 0 when it finished cleanly.
	failed int
}

// commandIndices returns the block index of every command block, in document
// order. Position in this slice is a command's *ordinal*.
//
// Ordinals are what a batch iterates over, never raw block indices: completing
// a cell splices an output block in after it, which shifts the index of every
// later block. A loop over indices captured up front would drift onto the wrong
// cells — or onto an output block — partway through. Ordinals are stable
// because output insertion never reorders commands relative to each other.
func commandIndices(nb *notebook.Notebook) []int {
	var out []int
	for i, b := range nb.Blocks {
		if _, ok := b.(notebook.CommandBlock); ok {
			out = append(out, i)
		}
	}
	return out
}

// ordinalOf returns the command ordinal for a block index, or -1 if that block
// is not a command.
func ordinalOf(nb *notebook.Notebook, blockIdx int) int {
	for ord, idx := range commandIndices(nb) {
		if idx == blockIdx {
			return ord
		}
	}
	return -1
}

// startBatch kicks off a batch beginning at the given command ordinal.
// The caller must hold s.mu and have verified nothing else is running.
func (s *Server) startBatch(startOrd int) {
	total := len(commandIndices(s.nb)) - startOrd
	if total < 0 {
		total = 0
	}
	s.batch = batchState{active: true, total: total}
	go s.runBatch(startOrd)
}

// runBatch executes commands from startOrd to the end of the notebook,
// stopping at the first non-zero exit.
//
// Stopping is not configurable: a notebook is usually a chain, and carrying on
// past a failed stage runs later cells against stale or absent inputs, which
// tends to produce plausible-looking wrong output rather than an error. A cell
// that should survive failure can say so in the shell — `cmd || true`.
func (s *Server) runBatch(startOrd int) {
	for ord := startOrd; ; ord++ {
		s.mu.Lock()
		if s.batch.abort {
			s.mu.Unlock()
			break
		}
		// Re-resolve the ordinal to a block index on every iteration: the
		// previous cell's output block shifted everything after it.
		idxs := commandIndices(s.nb)
		if ord >= len(idxs) {
			s.mu.Unlock()
			break
		}
		idx := idxs[ord]
		cmd, ok := s.nb.Blocks[idx].(notebook.CommandBlock)
		if !ok {
			s.mu.Unlock()
			break
		}
		body := cmd.Body(s.nb.Source)
		outType := cmd.Attrs["out"]
		if outType == "" {
			outType = "text"
		}
		s.activeIdx = idx
		s.mu.Unlock()

		res, runErr := s.runner.Run(context.Background(), body)

		s.mu.Lock()
		failed := s.applyResult(idx, res, runErr, outType)
		s.activeIdx = -1
		s.batch.completed++
		if failed {
			s.batch.failed = s.batch.completed
			s.batch.abort = true
		}
		stop := s.batch.abort
		s.mu.Unlock()

		if stop {
			break
		}
	}

	s.mu.Lock()
	s.batch.active = false
	s.mu.Unlock()
}

// batchStatus renders the progress label, empty when no batch is running.
func (s *Server) batchStatus() string {
	if !s.batch.active {
		return ""
	}
	return fmt.Sprintf("Running cell %d of %d", s.batch.completed+1, s.batch.total)
}
