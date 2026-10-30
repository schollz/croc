package bundle

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
)

func TestSharedFixtures(t *testing.T) {
	data, err := os.ReadFile("../../testdata/bundles/v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Feature string
		Members []struct {
			Text   string
			Offset int64
			XXH64  string
		}
		Payload string
		XXH64   string
		Plans   []struct {
			Name     string
			Sizes    []int64
			Eligible []bool
			Limits   Limits
			Expected [][]int
		}
		InvalidRequests []json.RawMessage
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Feature != Feature {
		t.Fatal("fixture feature mismatch")
	}
	hash := func(text string) string {
		h := xxhash.New()
		h.WriteString(text)
		return hex.EncodeToString(h.Sum(nil))
	}
	var payload string
	for _, member := range fixture.Members {
		if int64(len(payload)) != member.Offset || hash(member.Text) != member.XXH64 {
			t.Fatal("member fixture mismatch")
		}
		payload += member.Text
	}
	if payload != fixture.Payload || hash(payload) != fixture.XXH64 {
		t.Fatal("payload fixture mismatch")
	}
	for _, plan := range fixture.Plans {
		t.Run(plan.Name, func(t *testing.T) {
			members := make([]Member, len(plan.Sizes))
			for i, size := range plan.Sizes {
				members[i] = Member{size, plan.Eligible[i], true}
			}
			got, err := Plan(members, plan.Limits)
			if err != nil || !reflect.DeepEqual(got, plan.Expected) {
				t.Fatalf("plan = %v, %v; want %v", got, err, plan.Expected)
			}
		})
	}
	for _, raw := range fixture.InvalidRequests {
		var request Request
		err := Decode(raw, &request)
		request.Limits = NativeLimits()
		if err == nil {
			_, _, err = ValidateRequest(request, []Member{{1, true, true}, {2, true, true}, {3, true, true}, {4, true, true}}, NativeLimits())
		}
		if err == nil {
			t.Fatalf("accepted invalid request %s", raw)
		}
	}
}

func TestLimitsAndSkippedFiles(t *testing.T) {
	for _, l := range []Limits{{0, 128}, {NativeMaxBytes + 1, 128}, {1, 3}, {1, 129}, {-1, 4}} {
		if l.Validate() == nil {
			t.Fatalf("accepted %v", l)
		}
	}
	got, err := Effective(NativeLimits(), Limits{BrowserMaxBytes, 64})
	if err != nil || got != (Limits{BrowserMaxBytes, 64}) {
		t.Fatal(got, err)
	}
	plan, err := Plan([]Member{{1, true, true}, {1, true, false}, {1, true, true}, {1, true, true}, {1, true, true}}, NativeLimits())
	if err != nil || !reflect.DeepEqual(plan, [][]int{{0, 2, 3, 4}}) {
		t.Fatal(plan, err)
	}
}

func TestLifecycle(t *testing.T) {
	var state Lifecycle
	if state.Advance(1, "offer") == nil {
		t.Fatal("unsolicited offer")
	}
	for id := uint32(1); id <= 2; id++ {
		if err := state.Request(id); err != nil {
			t.Fatal(err)
		}
		if state.Request(id+1) == nil || state.Advance(id+1, "offer") == nil || state.Advance(id, "ready") == nil {
			t.Fatal("accepted invalid transition")
		}
		for _, op := range []string{"offer", "ready", "done", "closed"} {
			if err := state.Advance(id, op); err != nil {
				t.Fatal(err)
			}
		}
		if state.Request(id) == nil {
			t.Fatal("reused ID")
		}
	}
}

func TestExactUniqueChunks(t *testing.T) {
	chunks, _ := NewChunks(ChunkSize*2 + 1)
	for _, bad := range []struct {
		position uint64
		length   int
	}{{1, 1}, {0, 1}, {uint64(ChunkSize), 1}, {uint64(ChunkSize * 2), 2}, {^uint64(0), 1}} {
		if _, err := chunks.Accept(bad.position, bad.length); err == nil {
			t.Fatal("accepted bad chunk", bad)
		}
	}
	if done, err := chunks.Accept(uint64(ChunkSize), int(ChunkSize)); done || err != nil {
		t.Fatal(done, err)
	}
	if _, err := chunks.Accept(uint64(ChunkSize), int(ChunkSize)); err == nil {
		t.Fatal("duplicate")
	}
	if done, err := chunks.Accept(uint64(ChunkSize*2), 1); done || err != nil {
		t.Fatal(done, err)
	}
	if done, err := chunks.Accept(0, int(ChunkSize)); !done || err != nil {
		t.Fatal(done, err)
	}
}

func TestControlBoundsAndOffer(t *testing.T) {
	for _, raw := range []string{strings.Repeat(" ", MaxControlBytes+1), `{"id":1,"path":"evil"}`, `{"id":1} {"id":2}`} {
		if Decode([]byte(raw), new(Ack)) == nil {
			t.Fatal("accepted bad control")
		}
	}
	good := Offer{1, 10, make([]byte, 8), false}
	if err := ValidateOffer(good, 1, 10, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Offer{{2, 10, make([]byte, 8), false}, {1, 11, make([]byte, 8), false}, {1, 10, nil, false}, {1, 10, make([]byte, 8), true}} {
		if ValidateOffer(bad, 1, 10, true) == nil {
			t.Fatal("accepted bad offer")
		}
	}
}

func TestNextPlansOnlyOneUnitAndReturnsLayout(t *testing.T) {
	calls := 0
	unit, err := Next(1000, 0, Limits{32, 4}, func(index int) (Member, error) {
		calls++
		if index >= 4 {
			t.Fatal("preflight escaped the current unit")
		}
		return Member{Size: 2, Eligible: true, Requested: true}, nil
	})
	if err != nil || calls != 4 || !reflect.DeepEqual(unit, Layout{Indices: []int{0, 1, 2, 3}, Offsets: []int64{0, 2, 4, 6}, Size: 8}) {
		t.Fatal(unit, calls, err)
	}
}
