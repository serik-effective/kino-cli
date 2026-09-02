package pipeline

import "testing"

// The free tier stops at page 10, so a wide catalogue slice is walked by
// lowering the vote ceiling. Getting the descent wrong either loops forever or
// silently drops the tail of the catalogue.
func TestNextCeiling(t *testing.T) {
	const full = 10 // kp.MaxTierPages

	cases := []struct {
		name                            string
		lowest, ceiling, minVotes, want int
		pages                           int
	}{
		{name: "band fit, nothing below", lowest: 40, ceiling: 0, minVotes: 10, pages: 6, want: 0},
		{name: "full band descends to its floor", lowest: 512, ceiling: 0, minVotes: 10, pages: full, want: 512},
		{name: "descends again inside a band", lowest: 88, ceiling: 512, minVotes: 10, pages: full, want: 88},
		{name: "reached the requested floor", lowest: 10, ceiling: 88, minVotes: 10, pages: full, want: 0},
		{name: "below the floor", lowest: 3, ceiling: 88, minVotes: 10, pages: full, want: 0},
		// The loop-forever case: every film in the band has the same vote count.
		{name: "flat band steps one below", lowest: 512, ceiling: 512, minVotes: 10, pages: full, want: 511},
		{name: "flat band at the floor stops", lowest: 11, ceiling: 11, minVotes: 10, pages: full, want: 0},
		{name: "empty band", lowest: -1, ceiling: 512, minVotes: 10, pages: full, want: 0},
		{name: "never below zero", lowest: 0, ceiling: 1, minVotes: 0, pages: full, want: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := nextCeiling(c.lowest, c.ceiling, c.minVotes, c.pages); got != c.want {
				t.Errorf("nextCeiling(lowest=%d, ceiling=%d, min=%d, pages=%d) = %d, want %d",
					c.lowest, c.ceiling, c.minVotes, c.pages, got, c.want)
			}
		})
	}
}

// Each step must be strictly below the one before it, or the walk never ends.
func TestCeilingAlwaysDescends(t *testing.T) {
	ceiling, guard := 0, 0
	for lowest := 5000; ; guard++ {
		if guard > 6000 {
			t.Fatal("ceiling stopped descending — the band walk would not terminate")
		}
		next := nextCeiling(lowest, ceiling, 10, 10)
		if next == 0 {
			break
		}
		if ceiling > 0 && next >= ceiling {
			t.Fatalf("ceiling %d did not descend below %d", next, ceiling)
		}
		ceiling = next
		// Worst case the source keeps reporting the same vote count.
		lowest = ceiling
	}
}

// A backfill spread over days has to say where it stopped. Without a resume
// point every day starts from the top of the catalogue and spends its whole
// quota re-reading pages it already paid for.
func TestBandWalkReportsAResumePoint(t *testing.T) {
	// nextCeiling is the arithmetic behind the resume hint: the value printed
	// is the lowest vote count folded in, and asking again with that as the
	// ceiling must land strictly below it, never on the same page forever.
	const full = 10
	lowest := 4202
	next := nextCeiling(lowest, 0, 10, full)
	if next != lowest {
		t.Fatalf("resume point %d, want the lowest seen %d", next, lowest)
	}
	// And a second stop at the same count still descends.
	if again := nextCeiling(lowest, next, 10, full); again >= next {
		t.Errorf("second stop at the same count did not descend: %d", again)
	}
}
