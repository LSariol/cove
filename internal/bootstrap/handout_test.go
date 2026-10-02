package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenForHandsOutTheProjectToken(t *testing.T) {
	g, now := testGate(t)
	if _, err := g.OpenFor(0, "lighthouse", "cove_abc"); err != nil {
		t.Fatal(err)
	}
	if st, _ := g.Status(); st.TokenName != "lighthouse" {
		t.Fatalf("status TokenName = %q", st.TokenName)
	}

	outcome, h, err := g.Claim(lighthouse)
	if err != nil || outcome != Granted || h != (Handout{TokenName: "lighthouse", Token: "cove_abc"}) {
		t.Fatalf("Claim = %s %+v %v", outcome, h, err)
	}

	// Within the grace period, the same address gets the same token again.
	*now = now.Add(time.Minute)
	if outcome, h, _ := g.Claim(lighthouse); outcome != Redelivered || h.Token != "cove_abc" {
		t.Fatalf("redelivery = %s %+v", outcome, h)
	}
	if outcome, h, _ := g.Claim(stranger); outcome != Locked || h.Token != "" {
		t.Fatalf("another address = %s %+v", outcome, h)
	}
}

// The project token is written to the state file only while someone can
// still receive it.
func TestProjectTokenIsRemovedFromTheStateFile(t *testing.T) {
	stateHas := func(g *Gate, s string) bool {
		data, _ := os.ReadFile(filepath.Join(g.dir, stateFile))
		return strings.Contains(string(data), s)
	}

	// After the grace period.
	g, now := testGate(t)
	g.OpenFor(0, "lighthouse", "cove_secret1")
	claim(t, g, lighthouse)
	if !stateHas(g, "cove_secret1") {
		t.Fatal("the token should be kept during the grace period")
	}
	*now = now.Add(GracePeriod + time.Second)
	if outcome := claim(t, g, lighthouse); outcome != Locked {
		t.Fatalf("after the grace period = %s", outcome)
	}
	if stateHas(g, "cove_secret1") {
		t.Fatal("the token is still in the state file after the grace period")
	}

	// After the window expired unused (noticed by status).
	g, now = testGate(t)
	g.OpenFor(time.Minute, "lighthouse", "cove_secret2")
	*now = now.Add(2 * time.Minute)
	if st, _ := g.Status(); !st.Expired {
		t.Fatal("window should have expired")
	}
	if stateHas(g, "cove_secret2") {
		t.Fatal("the token is still in the state file after the window expired")
	}

	// After a lock.
	g, _ = testGate(t)
	g.OpenFor(0, "lighthouse", "cove_secret3")
	g.Lock()
	if stateHas(g, "cove_secret3") || stateHas(g, "lighthouse") {
		t.Fatal("lock left the token in the state file")
	}
}

// Opening again ends the previous grace period, so the old handout (perhaps a
// token that has since been rotated) can't be fetched again.
func TestOpenEndsThePreviousGracePeriod(t *testing.T) {
	g, _ := testGate(t)
	g.OpenFor(0, "lighthouse", "cove_old")
	claim(t, g, lighthouse)

	g.OpenFor(0, "marquee", "cove_new")
	_, h, _ := g.Claim(lighthouse)
	if h.Token != "cove_new" {
		t.Fatalf("got %+v, want the new handout", h)
	}
}
