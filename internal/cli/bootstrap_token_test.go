package cli

import (
	"context"
	"net/netip"
	"strings"
	"testing"

	"github.com/LSariol/Cove/internal/bootstrap"
)

func TestBootstrapOpenForAProject(t *testing.T) {
	ctx := context.Background()
	c, m := newTokenCLI(t, "")
	c.bootstrap = bootstrap.NewGate(t.TempDir(), nil)
	o, _, _ := run(t, c, "token create lighthouse --allow lighthouse.*")
	oldValue := strings.TrimSpace(o)

	_, e, err := run(t, c, "bootstrap open lighthouse 30m")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"(30m0s) to hand out a new token for lighthouse", "lighthouse's previous token stopped working"} {
		if !strings.Contains(e, want) {
			t.Errorf("stderr is missing %q:\n%s", want, e)
		}
	}
	if _, err := m.Authenticate(ctx, oldValue); err == nil {
		t.Error("the previous token still works")
	}

	o, _, _ = run(t, c, "bootstrap status")
	if !strings.Contains(o, "Hands out:     lighthouse's token") {
		t.Errorf("status = %q", o)
	}

	// The duration can come first too.
	if _, _, err := run(t, c, "bootstrap open 15m lighthouse"); err != nil {
		t.Fatal(err)
	}

	// The token the gate hands out is the new one.
	_, handout, _ := c.bootstrap.Claim(netip.MustParseAddr("172.18.0.5"))
	if tok, err := m.Authenticate(ctx, handout.Token); err != nil || tok.Name != "lighthouse" {
		t.Fatalf("handed-out token: %+v, %v", tok, err)
	}
}

func TestBootstrapOpenForAnUnknownProject(t *testing.T) {
	c, _ := newTokenCLI(t, "")
	c.bootstrap = bootstrap.NewGate(t.TempDir(), nil)

	_, _, err := run(t, c, "bootstrap open marquee")
	if err == nil || !strings.Contains(err.Error(), "token create marquee") {
		t.Fatalf("err = %v, want a hint to create the token", err)
	}
	if st, _ := c.bootstrap.Status(); st.Open {
		t.Fatal("the endpoint opened anyway")
	}
}
