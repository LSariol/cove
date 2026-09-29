package bootstrap

import (
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testGate returns a Gate with a controllable clock.
func testGate(t *testing.T, allowed ...string) (*Gate, *time.Time) {
	t.Helper()
	var prefixes []netip.Prefix
	for _, a := range allowed {
		prefixes = append(prefixes, netip.MustParsePrefix(a))
	}

	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	g := NewGate(t.TempDir(), prefixes)
	g.now = func() time.Time { return now }
	return g, &now
}

var (
	lighthouse = netip.MustParseAddr("172.18.0.5")
	stranger   = netip.MustParseAddr("192.168.1.50")
)

func claim(t *testing.T, g *Gate, addr netip.Addr) Outcome {
	t.Helper()
	outcome, _, err := g.Claim(addr)
	if err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestClosedByDefault(t *testing.T) {
	g, _ := testGate(t)

	if got := claim(t, g, lighthouse); got != Locked {
		t.Fatalf("fresh gate = %s, want locked", got)
	}
	if st, _ := g.Status(); st.Open {
		t.Fatal("fresh gate reports open")
	}
}

func TestOpenHandsOutOnceThenLocks(t *testing.T) {
	g, now := testGate(t)
	until, err := g.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	if want := now.Add(DefaultWindow); !until.Equal(want) {
		t.Fatalf("Open until %v, want %v (default window)", until, want)
	}

	if got := claim(t, g, lighthouse); got != Granted {
		t.Fatalf("first claim = %s, want granted", got)
	}
	if got := claim(t, g, stranger); got != Locked {
		t.Fatalf("second caller = %s, want locked", got)
	}

	st, _ := g.Status()
	if st.Open || st.LastHandoutTo != lighthouse.String() || !st.LastHandoutAt.Equal(*now) {
		t.Fatalf("status after handout = %+v", st)
	}
}

func TestGracePeriodForTheSameAddress(t *testing.T) {
	g, now := testGate(t)
	_, _ = g.Open(0)
	claim(t, g, lighthouse)

	*now = now.Add(GracePeriod - time.Second)
	if got := claim(t, g, lighthouse); got != Redelivered {
		t.Fatalf("same address within grace = %s, want redelivered", got)
	}
	if got := claim(t, g, stranger); got != Locked {
		t.Fatalf("other address within grace = %s, want locked", got)
	}

	*now = now.Add(2 * time.Second)
	if got := claim(t, g, lighthouse); got != Locked {
		t.Fatalf("same address after grace = %s, want locked", got)
	}
}

func TestWindowExpires(t *testing.T) {
	g, now := testGate(t)
	_, _ = g.Open(5 * time.Minute)

	*now = now.Add(5 * time.Minute)
	if got := claim(t, g, lighthouse); got != Expired {
		t.Fatalf("claim after the window = %s, want expired", got)
	}
	if st, _ := g.Status(); st.Open || !st.Expired {
		t.Fatalf("status after expiry = %+v", st)
	}
}

func TestLockClosesImmediately(t *testing.T) {
	g, _ := testGate(t)
	_, _ = g.Open(0)
	if err := g.Lock(); err != nil {
		t.Fatal(err)
	}
	if got := claim(t, g, lighthouse); got != Locked {
		t.Fatalf("claim after lock = %s, want locked", got)
	}

	// Lock also ends a grace period.
	_, _ = g.Open(0)
	claim(t, g, lighthouse)
	_ = g.Lock()
	if got := claim(t, g, lighthouse); got != Locked {
		t.Fatalf("claim in a locked grace period = %s, want locked", got)
	}
}

func TestAllowedNetworks(t *testing.T) {
	g, _ := testGate(t, "172.18.0.0/16")
	_, _ = g.Open(0)

	if got := claim(t, g, stranger); got != Forbidden {
		t.Fatalf("address outside the allowed networks = %s, want forbidden", got)
	}
	if got := claim(t, g, lighthouse); got != Granted {
		t.Fatalf("allowed address = %s, want granted (a forbidden request mustn't close the window)", got)
	}
}

func TestStateSurvivesANewGate(t *testing.T) {
	g, _ := testGate(t)
	_, _ = g.Open(0)

	// The CLI and the server are separate processes sharing the state file.
	other := NewGate(g.dir, nil)
	other.now = g.now
	if got := claim(t, other, lighthouse); got != Granted {
		t.Fatalf("claim through a second gate = %s, want granted", got)
	}
}

func TestLegacyMarkerIsReplaced(t *testing.T) {
	g, _ := testGate(t)
	legacy := filepath.Join(g.dir, legacyMarker)
	if err := os.WriteFile(legacy, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	_ = g.Lock()
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("the v0.2.0 marker file wasn't removed")
	}
}

func TestCorruptStateFailsClosed(t *testing.T) {
	g, _ := testGate(t)
	if err := os.WriteFile(filepath.Join(g.dir, stateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.Claim(lighthouse); err == nil {
		t.Fatal("Claim with a corrupt state file succeeded")
	}
}
