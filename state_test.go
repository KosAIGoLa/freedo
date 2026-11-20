package main

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"
	"time"
)

func proc(addr, circuits string) *Processor {
	return &Processor{Address: addr, Circuits: circuits}
}

func TestLeaderboardSorting(t *testing.T) {
	st := NewState(factoryAddr, "latest")
	now := time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)
	st.Merge([]*Processor{
		proc("0x0000000000000000000000000000000000000002", "5"),
		proc("0x0000000000000000000000000000000000000001", "1000"),
		proc("0x0000000000000000000000000000000000000003", "100"),
		proc("0x0000000000000000000000000000000000000004", "0"),
		// Tie on circuits: lower address first.
		proc("0x0000000000000000000000000000000000000006", "100"),
		proc("0x0000000000000000000000000000000000000005", "100"),
	}, 74007424, now)

	got := st.Leaderboard()
	want := []string{"1000", "100", "100", "100", "5", "0"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Circuits != w {
			t.Fatalf("rank %d: circuits = %s, want %s", i, got[i].Circuits, w)
		}
	}
	// Tie order among the three 100-circuit processors is by address ascending.
	tied := []string{got[1].Address, got[2].Address, got[3].Address}
	if tied[0] != "0x0000000000000000000000000000000000000003" ||
		tied[1] != "0x0000000000000000000000000000000000000005" ||
		tied[2] != "0x0000000000000000000000000000000000000006" {
		t.Fatalf("tie order = %v, want 03, 05, 06", tied)
	}
}

func TestStats(t *testing.T) {
	st := NewState(factoryAddr, "latest")
	now := time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)
	// Circuits: 6424, 1300, 1022, 2, 0, 0, 0 -> total 8748, median 2,
	// top-3 share (6424+1300+1022)/8748 = 99.9771%.
	st.Merge([]*Processor{
		proc("0x0000000000000000000000000000000000000001", "6424"),
		proc("0x0000000000000000000000000000000000000002", "1300"),
		proc("0x0000000000000000000000000000000000000003", "1022"),
		proc("0x0000000000000000000000000000000000000004", "2"),
		proc("0x0000000000000000000000000000000000000005", "0"),
		proc("0x0000000000000000000000000000000000000006", "0"),
		proc("0x0000000000000000000000000000000000000007", "0"),
	}, 74007424, now)

	s := st.Stats()
	if s.Processors != 7 {
		t.Fatalf("processors = %d, want 7", s.Processors)
	}
	if s.TotalCircuits != "8748" {
		t.Fatalf("total = %s, want 8748", s.TotalCircuits)
	}
	if s.ZeroCircuits != 3 {
		t.Fatalf("zero = %d, want 3", s.ZeroCircuits)
	}
	if s.MedianCircuits != "2" {
		t.Fatalf("median = %s, want 2", s.MedianCircuits)
	}
	if s.Top3SharePct < 99.97 || s.Top3SharePct > 99.98 {
		t.Fatalf("top3 share = %v, want ~99.977", s.Top3SharePct)
	}
	if len(s.TopHolders) != 3 || s.TopHolders[0].Circuits != "6424" {
		t.Fatalf("top holders = %+v", s.TopHolders)
	}
}

func TestStatsMedianEven(t *testing.T) {
	st := NewState(factoryAddr, "latest")
	now := time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)
	// Even count: values 0, 2, 4, 10 -> median (2+4)/2 = 3.
	st.Merge([]*Processor{
		proc("0x0000000000000000000000000000000000000001", "0"),
		proc("0x0000000000000000000000000000000000000002", "2"),
		proc("0x0000000000000000000000000000000000000003", "4"),
		proc("0x0000000000000000000000000000000000000004", "10"),
	}, 74007424, now)
	if got := st.Stats().MedianCircuits; got != "3" {
		t.Fatalf("median = %s, want 3", got)
	}
}

func TestMergeAlerts(t *testing.T) {
	st := NewState(factoryAddr, "latest")
	t0 := time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)
	t1 := t0.Add(5 * time.Minute)

	// Bootstrap sync: no alerts (nothing to compare against).
	a := st.Merge([]*Processor{
		proc("0x0000000000000000000000000000000000000001", "10"),
		proc("0x0000000000000000000000000000000000000002", "0"),
	}, 74007424, t0)
	if len(a) != 0 {
		t.Fatalf("bootstrap alerts = %d, want 0", len(a))
	}

	// Second sync: one new processor, one circuit increase, one unchanged.
	a = st.Merge([]*Processor{
		proc("0x0000000000000000000000000000000000000001", "12"),
		proc("0x0000000000000000000000000000000000000002", "0"),
		proc("0x0000000000000000000000000000000000000003", "4"),
	}, 74007430, t1)
	if len(a) != 2 {
		t.Fatalf("alerts = %d, want 2 (1 new processor + 1 circuit growth)", len(a))
	}
	kinds := map[string]bool{}
	for _, al := range a {
		kinds[al.Kind] = true
		if al.Kind == AlertNewCircuits && !strings.Contains(al.Detail, "10 -> 12 (+2)") {
			t.Fatalf("circuit alert detail = %q, want 10 -> 12 (+2)", al.Detail)
		}
	}
	if !kinds[AlertNewProcessor] || !kinds[AlertNewCircuits] {
		t.Fatalf("kinds = %v, want both new_processor and new_circuits", kinds)
	}

	// Third sync, no changes: no alerts.
	a = st.Merge([]*Processor{
		proc("0x0000000000000000000000000000000000000001", "12"),
		proc("0x0000000000000000000000000000000000000002", "0"),
		proc("0x0000000000000000000000000000000000000003", "4"),
	}, 74007440, t1.Add(5*time.Minute))
	if len(a) != 0 {
		t.Fatalf("no-change alerts = %d, want 0", len(a))
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	st := NewState(factoryAddr, "latest")
	now := time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)
	st.Merge([]*Processor{
		{Address: "0x0000000000000000000000000000000000000001", Index: 0, Circuits: "6424", Transistor: "0x0000000000000000000000000000000000000009", Price: "100", Minted: "50", Cap: "1000"},
		{Address: "0x0000000000000000000000000000000000000002", Index: 1, Circuits: "0"},
	}, 74007424, now)
	st.Merge([]*Processor{
		{Address: "0x0000000000000000000000000000000000000001", Index: 0, Circuits: "6430"},
		{Address: "0x0000000000000000000000000000000000000002", Index: 1, Circuits: "0"},
		{Address: "0x0000000000000000000000000000000000000003", Index: 2, Circuits: "7"},
	}, 74007430, now.Add(5*time.Minute))

	data, err := json.Marshal(st.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	st2 := NewState("", "")
	st2.Restore(snap)
	if st2.Stats().Processors != 3 {
		t.Fatalf("restored processors = %d, want 3", st2.Stats().Processors)
	}
	if st2.Stats().TotalCircuits != "6437" {
		t.Fatalf("restored total = %s, want 6437", st2.Stats().TotalCircuits)
	}
	p, ok := st2.GetProcessor("0X0000000000000000000000000000000000000001")
	if !ok {
		t.Fatal("GetProcessor (mixed case) not found")
	}
	if p.Transistor != "0x0000000000000000000000000000000000000009" {
		t.Fatalf("restored transistor = %s", p.Transistor)
	}
	if len(p.History) != 2 {
		t.Fatalf("history points = %d, want 2", len(p.History))
	}
	if len(st2.Alerts(time.Time{}, 0)) != 2 {
		t.Fatalf("restored alerts = %d, want 2", len(st2.Alerts(time.Time{}, 0)))
	}
	// big.Int sanity: total must survive as an exact decimal string.
	if _, ok := new(big.Int).SetString(st2.Stats().TotalCircuits, 10); !ok {
		t.Fatal("total circuits not a valid decimal")
	}
}
