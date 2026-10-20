package store_test

import (
	"context"
	"testing"
)

// Two hosts allocate the same project number from their own database. The host
// token keeps ids distinct, so id-keyed PR and review facts cannot collide.
func TestSessionIDsAreScopedPerHost(t *testing.T) {
	ctx := context.Background()
	hostA := newTestStore(t)
	hostB := newTestStore(t)
	hostA.SetSessionIDHostToken("haaa111")
	hostB.SetSessionIDHostToken("hbbb222")
	seedProject(t, hostA, "mer")
	seedProject(t, hostB, "mer")

	firstA, err := hostA.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}
	firstB, err := hostB.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}
	secondA, err := hostA.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}

	if firstA.ID != "mer-haaa111-1" || secondA.ID != "mer-haaa111-2" {
		t.Fatalf("host A ids = %q, %q", firstA.ID, secondA.ID)
	}
	if firstB.ID != "mer-hbbb222-1" {
		t.Fatalf("host B id = %q", firstB.ID)
	}
	if firstA.ID == firstB.ID {
		t.Fatalf("two hosts share session id %q", firstA.ID)
	}
	if _, ok, err := hostB.GetSession(ctx, firstA.ID); err != nil || ok {
		t.Fatalf("host B resolved host A's session: ok=%v err=%v", ok, err)
	}
}

// A store with no host identity keeps the legacy layout. A session written
// before the upgrade still loads while new sessions take the scoped format.
func TestSessionIDsKeepLegacyRowsWorking(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	seedProject(t, st, "mer")

	legacy, err := st.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}
	if legacy.ID != "mer-1" {
		t.Fatalf("legacy id = %q, want mer-1", legacy.ID)
	}

	st.SetSessionIDHostToken("haaa111")
	scoped, err := st.CreateSession(ctx, sampleRecord("mer"))
	if err != nil {
		t.Fatal(err)
	}
	if scoped.ID != "mer-haaa111-2" {
		t.Fatalf("scoped id = %q, want mer-haaa111-2", scoped.ID)
	}
	got, ok, err := st.GetSession(ctx, legacy.ID)
	if err != nil || !ok || got.ID != legacy.ID {
		t.Fatalf("legacy session after upgrade: %+v ok=%v err=%v", got, ok, err)
	}
}

func TestNextSessionIDStandaloneHostScoped(t *testing.T) {
	st := newTestStore(t)
	st.SetSessionIDHostToken("haaa111")
	rec, err := st.CreateSession(context.Background(), sampleRecord(""))
	if err != nil {
		t.Fatal(err)
	}
	if rec.ID != "standalone-haaa111-1" {
		t.Fatalf("standalone id = %q", rec.ID)
	}
}
