// Copyright (C) 2026 Luke Holland
// SPDX-License-Identifier: GPL-3.0-or-later

package resolve

import (
	"runtime"
	"testing"
	"time"

	"github.com/LukeHollandDev/palworld-save-reader/internal/savefixtures"
)

// This file is memory rule R4: the rule that makes the other three enforceable.
//
// R1 (iterate, never collect), R2 (one pass per collection) and R3 (stream the
// output) are all invisible in a passing test suite -- a resolve that collected
// everything eagerly would return exactly the same documents, just slowly and in
// gigabytes. The projection layer already demonstrates where that ends: --schema on
// the same world save allocates 1.9GB and then fails at its node ceiling. So the
// budget is asserted rather than hoped for.

// heapBudget is the peak live heap a resolve of the whole fixture save set may
// reach, in bytes.
//
// The figure is the measured peak with headroom, not a target: on the fixture world
// (a 1.0.1.100619 save with 9 players, 3,349 character records and tens of thousands
// of RawData blobs) a full resolve peaks at around 140MB, most of which is the
// decompressed world archive itself. The budget is roughly twice that, so an
// ordinary Go release or a somewhat larger world does not fail the build.
//
// What this catches is an R1 violation: calling Entries or Values on a Level.sav
// collection, which materialises every slot of every container at once and is how
// the projection layer reaches 1.9GB on the same file. It would not catch decoding
// every character record rather than the wanted ones -- 2,259 property streams is
// only tens of megabytes -- so that discipline is checked separately, by measuring
// allocations in TestResolvingOnePlayerCostsLessThanAll.
const heapBudget = 320 << 20

// TestResolveStaysWithinItsHeapBudget resolves every player in the fixture set
// while sampling the heap.
func TestResolveStaysWithinItsHeapBudget(t *testing.T) {
	set, err := Discover(savefixtures.Root(t))
	if err != nil {
		t.Fatal(err)
	}

	var players, pals int
	peak := peakHeap(t, func() {
		resolver, err := Open(set, Options{})
		if err != nil {
			t.Error(err)
			return
		}
		if err := resolver.Players(func(player *Player) error {
			players++
			pals += len(player.Pals)
			return nil
		}); err != nil {
			t.Error(err)
		}
	})

	t.Logf("peak heap = %d MiB resolving %d players and %d pals (budget %d MiB)",
		peak>>20, players, pals, heapBudget>>20)
	if players == 0 {
		t.Fatal("no players were resolved, so the measurement means nothing")
	}
	if peak > heapBudget {
		t.Errorf("peak heap %d MiB exceeds the %d MiB budget: something is being decoded or collected eagerly",
			peak>>20, heapBudget>>20)
	}
}

// TestResolvingOnePlayerCostsLessThanAll is the shape of the claim R2 makes about
// work rather than about memory: a resolve reads only the records it needs, so
// asking for one player out of nine must not decode every pal in the world.
//
// It is measured as allocated bytes rather than time, which is stable enough to
// assert on: the world archive is parsed either way, and the difference is the
// nested property streams. A resolver that decoded every record regardless would
// allocate the same for both.
func TestResolvingOnePlayerCostsLessThanAll(t *testing.T) {
	set, err := Discover(savefixtures.Root(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var first *Player
	all := allocatedBy(t, func() {
		if err := resolver.Players(func(player *Player) error {
			// The player with the fewest pals is the sharpest comparison: if the
			// wanted-id filter works, resolving them decodes almost nothing.
			if first == nil || len(player.Pals) < len(first.Pals) {
				first = player
			}
			return nil
		}); err != nil {
			t.Error(err)
		}
	})
	if first == nil {
		t.Skip("the fixture set has no players to measure against")
	}
	one := allocatedBy(t, func() {
		if _, err := resolver.Player(first.PlayerUID); err != nil {
			t.Error(err)
		}
	})

	t.Logf("resolving all players allocated %d MiB; resolving the smallest of them alone allocated %d MiB",
		all>>20, one>>20)
	if one >= all {
		t.Errorf("resolving one player allocated %d bytes and resolving every player allocated %d: the wanted-id filter is not saving any work",
			one, all)
	}
}

// TestResolveGuildsStaysWithinItsHeapBudget is the same budget for the mode phase
// 4 added. A guild resolve reads four collections rather than three and decodes the
// base camps' workers, so it is the mode most likely to reach for Entries on a
// container -- which is exactly what this would catch.
func TestResolveGuildsStaysWithinItsHeapBudget(t *testing.T) {
	set, err := Discover(savefixtures.Root(t))
	if err != nil {
		t.Fatal(err)
	}

	var guilds, bases, workers int
	peak := peakHeap(t, func() {
		resolver, err := Open(set, Options{})
		if err != nil {
			t.Error(err)
			return
		}
		if err := resolver.Guilds(func(guild *Guild) error {
			guilds++
			bases += len(guild.Bases)
			workers += guild.Counts.Workers
			return nil
		}); err != nil {
			t.Error(err)
		}
	})

	t.Logf("peak heap = %d MiB resolving %d guilds, %d bases and %d workers (budget %d MiB)",
		peak>>20, guilds, bases, workers, heapBudget>>20)
	if guilds == 0 {
		t.Fatal("no guilds were resolved, so the measurement means nothing")
	}
	if peak > heapBudget {
		t.Errorf("peak heap %d MiB exceeds the %d MiB budget: something is being decoded or collected eagerly",
			peak>>20, heapBudget>>20)
	}
}

// TestResolvingOneGuildCostsLessThanAll is R2 for guilds: the wanted-id filter is
// applied to the groups, and everything the later passes decode follows from it, so
// one guild must cost less than eight.
func TestResolvingOneGuildCostsLessThanAll(t *testing.T) {
	set, err := Discover(savefixtures.Root(t))
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := Open(set, Options{})
	if err != nil {
		t.Fatal(err)
	}

	var smallest *Guild
	all := allocatedBy(t, func() {
		if err := resolver.Guilds(func(guild *Guild) error {
			if smallest == nil || guild.Counts.Workers < smallest.Counts.Workers {
				smallest = guild
			}
			return nil
		}); err != nil {
			t.Error(err)
		}
	})
	if smallest == nil {
		t.Skip("the fixture world has no guilds to measure against")
	}
	one := allocatedBy(t, func() {
		if _, err := resolver.Guild(smallest.GroupID); err != nil {
			t.Error(err)
		}
	})

	t.Logf("resolving all guilds allocated %d MiB; resolving the smallest alone allocated %d MiB",
		all>>20, one>>20)
	if one >= all {
		t.Errorf("resolving one guild allocated %d bytes and resolving every guild allocated %d: the wanted-id filter is not saving any work",
			one, all)
	}
}

// peakHeap runs work while sampling the live heap, and returns the largest sample.
//
// Sampling is the only honest way to measure this: the interesting number is how
// much is alive at once, and by the time work returns the garbage collector has
// usually already taken it away.
func peakHeap(t *testing.T, work func()) uint64 {
	t.Helper()
	runtime.GC()

	done := make(chan struct{})
	peak := make(chan uint64, 1)
	go func() {
		var highest uint64
		var stats runtime.MemStats
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > highest {
					highest = stats.HeapAlloc
				}
				peak <- highest
				return
			case <-ticker.C:
				runtime.ReadMemStats(&stats)
				if stats.HeapAlloc > highest {
					highest = stats.HeapAlloc
				}
			}
		}
	}()

	work()
	close(done)
	return <-peak
}

// allocatedBy reports the total bytes allocated during work, which counts every
// allocation whether or not it survived. That is the measure of how much decoding
// happened, which is what R2 is about.
func allocatedBy(t *testing.T, work func()) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	work()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
